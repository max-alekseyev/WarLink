package singbox

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"time"
)

// APIHTTPClient provides connection pooling, HTTP/2 multiplexing, and persistent Keep-Alive
// to eliminate cold TLS handshakes, socket exhaustion, and request contention across all endpoints.
var APIHTTPClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   50,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	},
}

type InAppNotification struct {
	ID          int64  `json:"id"`
	TargetType  string `json:"target_type"` // "broadcast" | "account" | "device"
	Title       string `json:"title"`
	Message     string `json:"message"`
	Severity    string `json:"severity"` // "info" | "update" | "warning" | "urgent"
	ActionLabel string `json:"action_label,omitempty"`
	ActionURL   string `json:"action_url,omitempty"`
	CreatedAt   string `json:"created_at"`
	IsRead      bool   `json:"is_read"`
}

type DonationHistoryItem struct {
	InvoiceID int    `json:"invoice_id"`
	AmountRub int    `json:"amount_rub"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

type SponsorCard struct {
	AccountNumber      string                `json:"account_number"`
	Nickname           string                `json:"nickname"`
	AvatarURL          string                `json:"avatar_url"`
	SteamID            string                `json:"steam_id"`
	Motto              string                `json:"motto"`
	JoinedDate         string                `json:"joined_date"`
	IsActive           bool                  `json:"is_active"`
	TotalDonated       int64                 `json:"total_donated_rub"`
	HideDonationAmount bool                  `json:"hide_donation_amount"`
	Donations          []DonationHistoryItem `json:"donations"`
	Progression        json.RawMessage       `json:"progression,omitempty"`
}

type ProfileInfo struct {
	AccountNumber   string                `json:"account_number"`
	Nickname        string                `json:"nickname"`
	AvatarURL       string                `json:"avatar_url"`
	SteamID         string                `json:"steam_id"`
	Motto           string                `json:"motto"`
	Tier            string                `json:"tier"` // "free" | "sponsor" | "admin"
	SponsorUntil    int64                 `json:"sponsor_until"`
	DaysRemaining   int                   `json:"days_remaining"`
	CreatedAt       string                `json:"created_at"`
	TotalDonatedRub int                   `json:"total_donated_rub"`
	DeviceCount     int                   `json:"device_count"`
	Donations       []DonationHistoryItem `json:"donations"`
	Progression     json.RawMessage       `json:"progression,omitempty"`
	DiscordID       string                `json:"discord_id"`
	DiscordTag      string                `json:"discord_tag"`
	IsDiscordLinked bool                  `json:"is_discord_linked"`
}

// GetNotifications fetches in-app notifications for the current account and device.
func GetNotifications(account, device string) ([]InAppNotification, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("адрес шлюза не настроен")
	}

	u, err := url.Parse(fmt.Sprintf("%s/api/v1/notifications", serverAPI))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("account", account)
	q.Set("device", device)
	q.Set("route_mode", GetNetworkRouteMode())
	u.RawQuery = q.Encode()

	resp, err := APIHTTPClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var notifs []InAppNotification
	if err := json.NewDecoder(resp.Body).Decode(&notifs); err != nil {
		return nil, err
	}
	return notifs, nil
}

// MarkNotificationRead records that the device has read the notification.
func MarkNotificationRead(device string, notifID int64) error {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return fmt.Errorf("адрес шлюза не настроен")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"device_id":       device,
		"notification_id": notifID,
	})

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/notifications/read", serverAPI), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := APIHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// GetSponsorsList returns active sponsors for the Hall of Fame.
func GetSponsorsList() ([]SponsorCard, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("адрес шлюза не настроен")
	}

	resp, err := APIHTTPClient.Get(fmt.Sprintf("%s/api/v1/sponsors", serverAPI))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var sponsors []SponsorCard
	if err := json.NewDecoder(resp.Body).Decode(&sponsors); err != nil {
		return nil, err
	}
	return sponsors, nil
}

// FetchProfile retrieves current profile and donation history from gateway server.
func FetchProfile(account, device string) (*ProfileInfo, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("адрес шлюза не настроен")
	}

	u, err := url.Parse(fmt.Sprintf("%s/api/v1/profile", serverAPI))
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if account != "" {
		q.Set("account", account)
	}
	if device != "" {
		q.Set("device", device)
	}
	u.RawQuery = q.Encode()

	resp, err := APIHTTPClient.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var p ProfileInfo
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// UpdateProfile updates nickname and links account to device, optionally updating steam_id and motto.
func UpdateProfile(account, device, nickname string, optionalExtra ...string) (*ProfileInfo, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil, fmt.Errorf("адрес шлюза не настроен")
	}

	payload := map[string]interface{}{
		"account_number": account,
		"device_id":      device,
		"nickname":       nickname,
	}
	if len(optionalExtra) > 0 && optionalExtra[0] != "" {
		payload["steam_id"] = optionalExtra[0]
	}
	if len(optionalExtra) > 1 && optionalExtra[1] != "" {
		payload["motto"] = optionalExtra[1]
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/profile", serverAPI), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := APIHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		var errData struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if errJSON := json.Unmarshal(b, &errData); errJSON == nil && errData.Message != "" {
			return nil, fmt.Errorf("%s", errData.Message)
		}
		return nil, fmt.Errorf("ошибка сохранения профиля: %s", string(b))
	}

	var p ProfileInfo
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// SyncProgression sends player progression to server so sponsors can share their progression in dossier.
func SyncProgression(account, device string, prog interface{}) error {
	serverAPI := GetServerAPI()
	if serverAPI == "" || account == "" || prog == nil {
		return nil
	}

	payload := map[string]interface{}{
		"account_number": account,
		"device_id":      device,
		"progression":    prog,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/profile", serverAPI), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := APIHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// ResetOtherDevices removes all other registered devices for the account on the server.
func ResetOtherDevices(account, device string) error {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]interface{}{
		"account_number": account,
		"device_id":      device,
		"action":         "reset_devices",
	})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/profile", serverAPI), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := APIHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// UploadAvatar sends the image to the server for gentle compression (128x128 WebP).
func UploadAvatar(account string, imgBytes []byte, filename string) (string, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return "", fmt.Errorf("адрес шлюза не настроен")
	}

	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	_ = w.WriteField("account_number", account)
	fw, err := w.CreateFormFile("avatar", filename)
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(imgBytes); err != nil {
		return "", err
	}
	_ = w.Close()

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/profile/avatar", serverAPI), &b)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := APIHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ошибка загрузки аватарки: %s", string(msg))
	}

	var result struct {
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.AvatarURL, nil
}


// CreateCustomDonationWithMethod creates a payment invoice for a specified payment method.
func CreateCustomDonationWithMethod(account, device string, amountRub int, paymentMethod string) (string, error) {
	minRub := 100
	if paymentMethod == "pawnow:usdt_trc20" || paymentMethod == "pawnow:usdt_ton" || paymentMethod == "pawnow:ton_ton" ||
		paymentMethod == "usdt_trc20" || paymentMethod == "usdt_ton" || paymentMethod == "ton" {
		minRub = 650
	}
	if amountRub < minRub {
		amountRub = minRub
	}
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return "", fmt.Errorf("адрес шлюза не настроен")
	}

	payload := map[string]interface{}{
		"account_number": account,
		"device_id":      device,
		"amount_rub":     amountRub,
	}
	if paymentMethod != "" {
		payload["payment_method"] = paymentMethod
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/v1/donate", serverAPI), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := APIHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ошибка создания счета: %s", string(b))
	}

	var res struct {
		PaymentURL string `json:"payment_url"`
		URL        string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", err
	}
	if res.PaymentURL != "" {
		return res.PaymentURL, nil
	}
	return res.URL, nil
}

// GetDiscordLinkCode requests a temporary 6-digit code for linking Discord account
func GetDiscordLinkCode(account, device string) (string, int, error) {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return "", 0, fmt.Errorf("адрес шлюза не настроен")
	}

	payload, _ := json.Marshal(map[string]string{
		"account_number": account,
		"device_id":      device,
	})

	resp, err := APIHTTPClient.Post(fmt.Sprintf("%s/api/v1/profile/discord/link-code", serverAPI), "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var res struct {
		Success   bool   `json:"success"`
		Code      string `json:"code"`
		ExpiresIn int    `json:"expires_in"`
		Error     string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return "", 0, err
	}
	if !res.Success {
		return "", 0, fmt.Errorf("%s", res.Error)
	}
	return res.Code, res.ExpiresIn, nil
}

// UnlinkDiscord requests server to unlink Discord account from WarLink profile
func UnlinkDiscord(account, device string) error {
	serverAPI := GetServerAPI()
	if serverAPI == "" {
		return fmt.Errorf("адрес шлюза не настроен")
	}

	payload, _ := json.Marshal(map[string]string{
		"account_number": account,
		"device_id":      device,
	})

	resp, err := APIHTTPClient.Post(fmt.Sprintf("%s/api/v1/profile/discord/unlink", serverAPI), "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("сервер вернул статус %d", resp.StatusCode)
	}

	var res struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}
	if !res.Success && res.Error != "" {
		return fmt.Errorf("%s", res.Error)
	}
	return nil
}

