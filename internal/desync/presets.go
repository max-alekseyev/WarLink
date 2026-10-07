package desync

import (
	"sync"
)

type Preset struct {
	Name string   `json:"name"`
	Args []string `json:"args,omitempty"`
}

var (
	presetsMu      sync.RWMutex
	BuiltinPresets = []Preset{
		{Name: "Автокалибровка (Circular Adaptive)"},
		{Name: "Прямой шлюз Hysteria 2"},
		{Name: "Транзитный шлюз Hysteria 2"},
	}
)

func GetAvailablePresetNames() []string {
	presetsMu.RLock()
	defer presetsMu.RUnlock()
	res := make([]string, len(BuiltinPresets))
	for i, p := range BuiltinPresets {
		res[i] = p.Name
	}
	return res
}

func GetPreset(name string) *Preset {
	presetsMu.RLock()
	defer presetsMu.RUnlock()
	for _, p := range BuiltinPresets {
		if p.Name == name {
			cp := p
			return &cp
		}
	}
	return nil
}

func (p *Preset) BuildArgs(_ string) []string {
	return []string{}
}

func (p *Preset) BuildModularArgs(_ string, _ bool) []string {
	return []string{}
}

func (p *Preset) BuildFilteredArgs(coreDir string, freeInternet bool) []string {
	return p.BuildModularArgs(coreDir, freeInternet)
}

func FetchRemoteDesyncConfig(serverAPI string) ([]Preset, error) {
	return nil, nil
}
