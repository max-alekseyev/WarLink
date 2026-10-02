package progression

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"math/bits"
	"os"

	"github.com/disintegration/imaging"
)

// RoleProgression stores recognized level and circle XP progress percentage.
type RoleProgression struct {
	Level      int     `json:"level"`
	XPProgress float64 `json:"xp_progress"` // 0.0 .. 100.0%
}

// ProgressionResult contains career level and all 6 role progression statuses.
type ProgressionResult struct {
	CareerLevel int                        `json:"career_level"`
	Roles       map[string]RoleProgression `json:"roles"`
	SumRoles    int                        `json:"sum_roles"`
	Valid       bool                       `json:"valid"`
}

// 16x24 binary bitmasks for digits 0-9. Each row of 16 pixels is a uint16.
var digitTemplates = [10][24]uint16{
	// Digit 0
	{0x0FF8, 0x3FFC, 0x3FFE, 0x7FFF, 0xFE3F, 0xFC1F, 0xF81F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF80F, 0xF81F, 0xFC1F, 0xFF7F, 0x7FFF, 0x3FFE, 0x1FFC, 0x07F0},
	// Digit 1 (handled by aspect ratio < 0.42, fallback template)
	{0x07FF, 0x7FFF, 0xFFFF, 0xFFFF, 0xFFFF, 0xF1FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF, 0x01FF},
	// Digit 2
	{0x0FE0, 0x3FF8, 0x7FFC, 0xFFFE, 0xF83E, 0xF81E, 0xF01E, 0xF01E, 0x003E, 0x003E, 0x007C, 0x00FC, 0x01F8, 0x03F0, 0x07E0, 0x07C0, 0x0F80, 0x1F00, 0x3E00, 0x7E00, 0xFFFE, 0xFFFF, 0xFFFF, 0xFFFF},
	// Digit 3
	{0x0FE0, 0x3FF8, 0x7FFC, 0xFFFE, 0x003E, 0x003E, 0x003E, 0x003E, 0x003E, 0x07FE, 0x0FFE, 0x0FFE, 0x07FE, 0x003E, 0x003E, 0x003E, 0x003E, 0x003E, 0xF83E, 0xFFFE, 0x7FFC, 0x3FF8, 0x1FF0, 0x0FE0},
	// Digit 4
	{0x00FE, 0x01FE, 0x01FE, 0x03FE, 0x03FE, 0x07FE, 0x0FBE, 0x0FBE, 0x1F3E, 0x1F3E, 0x3E3E, 0x7C3E, 0x7C3E, 0xF83E, 0xFFFF, 0xFFFF, 0xFFFF, 0xFFFF, 0x003E, 0x003E, 0x003E, 0x003E, 0x003E, 0x003E},
	// Digit 5
	{0xFFFE, 0xFFFE, 0xFFFE, 0xFFFE, 0xF800, 0xF800, 0xF800, 0xF800, 0xFBF0, 0xFFF8, 0xFFFC, 0xFFFE, 0xFE7F, 0xFC3F, 0x001F, 0x001F, 0x001F, 0xF81F, 0xFC3F, 0xFFFF, 0x7FFE, 0x3FFC, 0x1FF8, 0x07E0},
	// Digit 6
	{0x0FE0, 0x3FF8, 0x7FFC, 0x7EFE, 0xF83E, 0xF83E, 0xF81E, 0xF800, 0xF800, 0xFFF0, 0xFFFC, 0xFFFC, 0xFC7E, 0xF83E, 0xF81E, 0xF81F, 0xF81F, 0xF81F, 0xF81E, 0x783E, 0x7FFE, 0x3FFC, 0x1FF8, 0x0FE0},
	// Digit 7
	{0xFFFF, 0xFFFF, 0xFFFF, 0xFA3F, 0xF03E, 0x003E, 0x003E, 0x007C, 0x007C, 0x007C, 0x00F8, 0x00F8, 0x01F0, 0x01F0, 0x01F0, 0x03E0, 0x03E0, 0x07E0, 0x07C0, 0x07C0, 0x0F80, 0x0F80, 0x0F80, 0x1F00},
	// Digit 8
	{0x0FE0, 0x3FF8, 0x7FFC, 0x7FFE, 0xF83E, 0xF81E, 0xF81E, 0xF81E, 0x7C3E, 0x7FFC, 0x3FF8, 0x3FFC, 0x7EFC, 0xF83E, 0xF81E, 0xF81F, 0xF81F, 0xF81F, 0xF83E, 0xFC3E, 0x7FFE, 0x7FFC, 0x3FF8, 0x0FE0},
	// Digit 9
	{0x0FF0, 0x3FF8, 0x7FFC, 0xFFFE, 0xF83F, 0xF81F, 0xF81F, 0xF81F, 0xF81F, 0xF81F, 0xF83F, 0xFFFF, 0x7FFF, 0x3FFF, 0x0FDF, 0x001F, 0x001F, 0xF81F, 0xF81F, 0xFC3F, 0x7FFF, 0x7FFE, 0x3FFC, 0x0FF0},
}

type roleConfig struct {
	Name string
	Rx   float64
	Ry   float64
}

var rolesGrid = []roleConfig{
	{Name: "assault", Rx: 0.068359, Ry: 0.336806},
	{Name: "medic",   Rx: 0.176563, Ry: 0.336806},
	{Name: "recon",   Rx: 0.068359, Ry: 0.528472},
	{Name: "support", Rx: 0.176563, Ry: 0.528472},
	{Name: "driver",  Rx: 0.068359, Ry: 0.720139},
	{Name: "pilot",   Rx: 0.176563, Ry: 0.720139},
}

// ParseScreenshotFile loads and parses a screenshot from the file path.
func ParseScreenshotFile(path string) (*ProgressionResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open screenshot: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("failed to decode image: %w", err)
	}
	return ParseScreenshot(img)
}

// ParseScreenshotBytes decodes image bytes and parses progression.
func ParseScreenshotBytes(data []byte) (*ProgressionResult, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image bytes: %w", err)
	}
	return ParseScreenshot(img)
}

// ParseScreenshot detects 16:9 canvas, isolates numbers, matches templates and measures XP arcs.
func ParseScreenshot(img image.Image) (*ProgressionResult, error) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w < 640 || h < 360 {
		return nil, errors.New("resolution too small for reliable detection")
	}

	canvasW, canvasH, xOff, yOff := calculateCanvas(w, h)

	// 1. Career Level with safety margin to prevent clipping top/bottom arches
	cx := xOff + int(math.Round(0.188*float64(canvasW)))
	cy := yOff + int(math.Round(0.208*float64(canvasH)))
	cw := int(math.Round(0.052 * float64(canvasW)))
	ch := int(math.Round(0.056 * float64(canvasH)))
	careerCrop := imaging.Crop(img, image.Rect(cx, cy, cx+cw, cy+ch))
	careerLevel := recognizeNumber(careerCrop)

	// 2. Roles Progression
	roles := make(map[string]RoleProgression, 6)
	sumRoles := 0

	for _, r := range rolesGrid {
		// Crop number box with adequate vertical clearance to include top curve of digits
		nx := xOff + int(math.Round((r.Rx+0.027344)*float64(canvasW)))
		ny := yOff + int(math.Round((r.Ry+0.107)*float64(canvasH)))
		nw := int(math.Round(0.041016 * float64(canvasW)))
		nh := int(math.Round(0.030 * float64(canvasH)))
		numCrop := imaging.Crop(img, image.Rect(nx, ny, nx+nw, ny+nh))
		lvl := recognizeNumber(numCrop)

		// Crop card for XP arc
		cardX := xOff + int(math.Round(r.Rx*float64(canvasW)))
		cardY := yOff + int(math.Round(r.Ry*float64(canvasH)))
		cardW := int(math.Round(0.095703 * float64(canvasW)))
		cardH := int(math.Round(0.175694 * float64(canvasH)))
		cardCrop := imaging.Crop(img, image.Rect(cardX, cardY, cardX+cardW, cardY+cardH))
		xpPct := calculateXPArc(cardCrop)

		roles[r.Name] = RoleProgression{
			Level:      lvl,
			XPProgress: math.Round(xpPct*10) / 10,
		}
		sumRoles += lvl
	}

	valid := (sumRoles == careerLevel && careerLevel > 0)
	if !valid && careerLevel > 0 && sumRoles > 0 && math.Abs(float64(careerLevel-sumRoles)) <= 2 {
		valid = true
	}

	return &ProgressionResult{
		CareerLevel: careerLevel,
		Roles:       roles,
		SumRoles:    sumRoles,
		Valid:       valid,
	}, nil
}

func calculateCanvas(w, h int) (canvasW, canvasH, xOff, yOff int) {
	targetAR := 16.0 / 9.0
	ar := float64(w) / float64(h)

	if ar > targetAR+0.01 { // Ultrawide (21:9, 32:9)
		canvasH = h
		canvasW = int(math.Round(float64(h) * targetAR))
		xOff = (w - canvasW) / 2
		yOff = 0
	} else if ar < targetAR-0.01 { // Pillarbox (16:10, 4:3)
		canvasW = w
		canvasH = int(math.Round(float64(w) / targetAR))
		xOff = 0
		yOff = (h - canvasH) / 2
	} else { // Standard 16:9
		canvasW = w
		canvasH = h
		xOff = 0
		yOff = 0
	}
	return
}

type spanRect struct {
	x0, x1 int
	y0, y1 int
}

func getValidSpans(crop image.Image) []spanRect {
	bounds := crop.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()

	colHasBright := make([]bool, w)
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			c := color.GrayModel.Convert(crop.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray)
			if c.Y > 120 {
				colHasBright[x] = true
				break
			}
		}
	}

	type rawSpan struct {
		start, end int
	}
	var raw []rawSpan
	inSpan := false
	sStart := 0
	for x, b := range colHasBright {
		if b && !inSpan {
			inSpan = true
			sStart = x
		} else if !b && inSpan {
			inSpan = false
			raw = append(raw, rawSpan{start: sStart, end: x})
		}
	}
	if inSpan {
		raw = append(raw, rawSpan{start: sStart, end: w})
	}

	var valid []spanRect
	for _, s := range raw {
		pixelCount := 0
		minY := h
		maxY := 0
		for x := s.start; x < s.end; x++ {
			for y := 0; y < h; y++ {
				c := color.GrayModel.Convert(crop.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray)
				if c.Y > 120 {
					pixelCount++
					if y < minY {
						minY = y
					}
					if y > maxY {
						maxY = y
					}
				}
			}
		}

		if pixelCount == 0 {
			continue
		}
		spanHeight := maxY - minY + 1
		// True digit requirement: at least 38% of crop height and sufficient pixel mass
		if float64(spanHeight) >= float64(h)*0.38 && pixelCount >= spanHeight*2 {
			valid = append(valid, spanRect{
				x0: s.start,
				x1: s.end,
				y0: minY,
				y1: maxY,
			})
		}
	}
	return valid
}

func recognizeNumber(crop image.Image) int {
	spans := getValidSpans(crop)
	if len(spans) == 0 {
		return 0
	}

	bounds := crop.Bounds()
	result := 0

	for _, s := range spans {
		wSpan := s.x1 - s.x0
		hSpan := s.y1 - s.y0 + 1
		aspect := float64(wSpan) / float64(max(1, hSpan))

		// In WARDOGS UI font, digit 1 is exceptionally narrow (aspect < 0.42)
		if aspect < 0.42 {
			result = result*10 + 1
			continue
		}

		charCrop := imaging.Crop(crop, image.Rect(
			bounds.Min.X+s.x0,
			bounds.Min.Y+s.y0,
			bounds.Min.X+s.x1,
			bounds.Min.Y+s.y1+1,
		))

		// Resize to 16x24
		norm := imaging.Resize(charCrop, 16, 24, imaging.Linear)
		normBounds := norm.Bounds()

		// Convert to 24xuint16 bitmask
		var bitsArr [24]uint16
		for y := 0; y < 24; y++ {
			var rowVal uint16
			for x := 0; x < 16; x++ {
				c := color.GrayModel.Convert(norm.At(normBounds.Min.X+x, normBounds.Min.Y+y)).(color.Gray)
				if c.Y > 120 {
					rowVal |= (1 << (15 - x))
				}
			}
			bitsArr[y] = rowVal
		}

		bestDigit := 0
		bestIoU := -1.0

		for d := 0; d < 10; d++ {
			if d == 1 {
				continue // 1 handled by aspect ratio
			}
			t := digitTemplates[d]
			interBits := 0
			unionBits := 0
			for y := 0; y < 24; y++ {
				interBits += bits.OnesCount16(bitsArr[y] & t[y])
				unionBits += bits.OnesCount16(bitsArr[y] | t[y])
			}
			iou := 0.0
			if unionBits > 0 {
				iou = float64(interBits) / float64(unionBits)
			}
			if iou > bestIoU {
				bestIoU = iou
				bestDigit = d
			}
		}
		result = result*10 + bestDigit
	}
	return result
}

func calculateXPArc(card image.Image) float64 {
	bounds := card.Bounds()
	cw := float64(bounds.Dx())
	ch := float64(bounds.Dy())

	// Scaled center and radius based on reference 245x253 card
	cx := cw * (122.5 / 245.0)
	cy := ch * (109.0 / 253.0)
	rTarget := cw * (86.0 / 245.0)

	totalSamples := 288
	yellowSamples := 0

	for deg := 0; deg < totalSamples; deg++ {
		angleDeg := float64((216 + deg) % 360)
		rad := angleDeg * (math.Pi / 180.0)

		isYellow := false
		for dr := -2.0; dr <= 2.0; dr += 1.0 {
			px := int(math.Round(cx + (rTarget+dr)*math.Cos(rad)))
			py := int(math.Round(cy + (rTarget+dr)*math.Sin(rad)))
			if px >= 0 && px < bounds.Dx() && py >= 0 && py < bounds.Dy() {
				r, g, b, _ := card.At(bounds.Min.X+px, bounds.Min.Y+py).RGBA()
				// RGBA returns 0..65535, scale to 0..255
				r8 := uint8(r >> 8)
				g8 := uint8(g >> 8)
				b8 := uint8(b >> 8)
				if r8 > 160 && g8 > 135 && int(r8)-int(b8) > 40 {
					isYellow = true
					break
				}
			}
		}
		if isYellow {
			yellowSamples++
		}
	}

	return (float64(yellowSamples) / float64(totalSamples)) * 100.0
}
