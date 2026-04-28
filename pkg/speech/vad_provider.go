package speech

import "fmt"

type VADProviderType string

const (
	VADProviderHeuristic VADProviderType = "heuristic"
)

type VADProcessor interface {
	Name() string
	Type() VADProviderType
	ProcessFrame(samples []int16) VADState
	ProcessFloatFrame(samples []float32) VADState
	RegisterListener(listener VADStateListener)
	State() VADState
	Reset()
	UpdateConfig(cfg VADConfig)
	Config() VADConfig
}

type VADProviderFactory func(cfg VADConfig) (VADProcessor, error)

type VADManager struct {
	factories map[VADProviderType]VADProviderFactory
}

func NewVADManager() *VADManager {
	m := &VADManager{
		factories: map[VADProviderType]VADProviderFactory{},
	}
	m.Register(VADProviderHeuristic, func(cfg VADConfig) (VADProcessor, error) {
		return NewVAD(cfg), nil
	})
	return m
}

func (m *VADManager) Register(providerType VADProviderType, factory VADProviderFactory) {
	m.factories[providerType] = factory
}

func (m *VADManager) New(cfg VADConfig, providerType VADProviderType) (VADProcessor, error) {
	if providerType == "" {
		providerType = VADProviderHeuristic
	}

	factory, ok := m.factories[providerType]
	if !ok {
		return nil, fmt.Errorf("vad: unsupported provider %q", providerType)
	}

	return factory(cfg)
}

