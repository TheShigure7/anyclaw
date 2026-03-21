package app

import (
	"fmt"

	"github.com/anyclaw/anyclaw/pkg/controlplane"
	"github.com/anyclaw/anyclaw/pkg/gateway"
	"github.com/anyclaw/anyclaw/pkg/orchestrator"
	"github.com/anyclaw/anyclaw/pkg/runtimecore"
	"github.com/anyclaw/anyclaw/pkg/security"
	"github.com/anyclaw/anyclaw/pkg/storage"
)

type App struct {
	ControlPlane *controlplane.Service
	Orchestrator *orchestrator.Service
	Runtime      *runtimecore.Engine
	Security     *security.Service
	Gateway      *gateway.Server
	Storage      *storage.LocalStore
}

func New() *App {
	store := storage.NewLocalStore(".anyclaw")
	securityService := security.NewService()
	runtimeEngine := runtimecore.NewEngine()
	orchestratorService := orchestrator.NewService(runtimeEngine, securityService, store)
	controlPlaneService := controlplane.NewService(store)
	gatewayServer := gateway.NewServer(controlPlaneService, orchestratorService)

	return &App{
		ControlPlane: controlPlaneService,
		Orchestrator: orchestratorService,
		Runtime:      runtimeEngine,
		Security:     securityService,
		Gateway:      gatewayServer,
		Storage:      store,
	}
}

func (a *App) Run() error {
	if a == nil {
		return fmt.Errorf("app is nil")
	}
	return a.Gateway.Start()
}
