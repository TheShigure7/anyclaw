package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	"github.com/anyclaw/anyclaw/pkg/config"
	"github.com/gorilla/websocket"
)

type openClawWSFrame struct {
	Type   string         `json:"type"`
	ID     string         `json:"id,omitempty"`
	Method string         `json:"method,omitempty"`
	Event  string         `json:"event,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Data   any            `json:"data,omitempty"`
	OK     bool           `json:"ok,omitempty"`
	Error  string         `json:"error,omitempty"`
}

type openClawWSConn struct {
	server      *Server
	conn        *websocket.Conn
	user        *AuthUser
	writeMu     sync.Mutex
	connected   bool
	connMu      sync.RWMutex
	challenge   string
	eventStream chan *Event
	closed      chan struct{}
	closeOnce   sync.Once
	connectedAt time.Time
}

var openClawWSMethods = []string{
	"connect",
	"ping",
	"methods.list",
	"status.get",
	"status",
	"control-plane.get",
	"control_plane.get",
	"agents.list",
	"agents.get",
	"agents.create",
	"agents.update",
	"agents.delete",
	"providers.list",
	"providers.get",
	"providers.update",
	"providers.default",
	"providers.test",
	"agent-bindings.list",
	"agent_bindings.list",
	"agent-bindings.update",
	"agent_bindings.update",
	"app-bindings.list",
	"app_bindings.list",
	"app-bindings.get",
	"app_bindings.get",
	"app-bindings.update",
	"app_bindings.update",
	"app-pairings.list",
	"app_pairings.list",
	"app-pairings.get",
	"app_pairings.get",
	"app-pairings.update",
	"app_pairings.update",
	"app-workflows.resolve",
	"app_workflows.resolve",
	"channels.list",
	"channels.status",
	"channels.login",
	"channels.logout",
	"sessions.list",
	"sessions.get",
	"sessions.create",
	"sessions.spawn",
	"sessions.delete",
	"sessions.history",
	"sessions.send",
	"sessions.abort",
	"sessions.patch",
	"sessions_abort",
	"sessions_spawn",
	"sessions_send",
	"tasks.list",
	"tasks.create",
	"tasks.get",
	"tasks.cancel",
	"apps.list",
	"apps.get",
	"surfaces.list",
	"surfaces.get",
	"tools.list",
	"tools.catalog",
	"tools.invoke",
	"tools_invoke",
	"plugins.list",
	"plugins.install",
	"plugins.uninstall",
	"events.list",
	"events.subscribe",
	"events.unsubscribe",
	"chat.send",
	"chat.inject",
	"chat.history",
	"chat.abort",
	"nodes.list",
	"nodes.get",
	"nodes.invoke",
	"node_invoke",
	"device.pairing.generate",
	"device.pairing.validate",
	"device.pairing.pair",
	"device.pairing.unpair",
	"device.pairing.list",
	"device.pairing.status",
	"device.pairing.renew",
	"config.get",
	"config.set",
	"config.patch",
	"config.schema",
	"canvas.list",
	"canvas.get",
	"canvas.push",
	"canvas.reset",
	"canvas.versions",
}

var openClawWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (s *Server) handleOpenClawWS(w http.ResponseWriter, r *http.Request) {
	conn, err := openClawWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &openClawWSConn{
		server:    s,
		conn:      conn,
		user:      UserFromContext(r.Context()),
		challenge: uniqueID("ws"),
		closed:    make(chan struct{}),
	}
	client.run(r.Context())
}

func (c *openClawWSConn) run(ctx context.Context) {
	defer c.shutdown()
	_ = c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})
	if err := c.writeFrame(openClawWSFrame{
		Type:  "event",
		Event: "connect.challenge",
		Data: map[string]any{
			"nonce":    c.challenge,
			"protocol": "openclaw.gateway.v1",
			"methods":  openClawWSMethods,
		},
	}); err != nil {
		return
	}
	var handlerWg sync.WaitGroup
	defer func() {
		handlerWg.Wait()
	}()
	for {
		var frame openClawWSFrame
		if err := c.conn.ReadJSON(&frame); err != nil {
			return
		}
		_ = c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		if !strings.EqualFold(strings.TrimSpace(frame.Type), "req") {
			_ = c.writeError(frame.ID, "frame type must be req")
			continue
		}
		handlerWg.Add(1)
		go func(f openClawWSFrame) {
			defer handlerWg.Done()
			if err := c.handleRequest(ctx, f); err != nil {
				_ = c.writeError(f.ID, err.Error())
			}
		}(frame)
	}
}

func (c *openClawWSConn) handleRequest(ctx context.Context, frame openClawWSFrame) error {
	method := strings.TrimSpace(frame.Method)
	if method == "" {
		return fmt.Errorf("method is required")
	}
	c.connMu.RLock()
	connected := c.connected
	c.connMu.RUnlock()
	if !connected && !strings.EqualFold(method, "connect") {
		return fmt.Errorf("connect required before calling %s", method)
	}
	switch strings.ToLower(method) {
	case "connect":
		provided := firstNonEmpty(mapString(frame.Params, "challenge"), mapString(frame.Params, "nonce"))
		if provided == "" || provided != c.challenge {
			return c.writeResponse(frame.ID, false, nil, "challenge verification failed")
		}
		c.connMu.Lock()
		c.connected = true
		c.connectedAt = time.Now().UTC()
		c.connMu.Unlock()
		return c.writeResponse(frame.ID, true, map[string]any{
			"status":       "connected",
			"protocol":     "openclaw.gateway.v1",
			"connected_at": c.connectedAt.Format(time.RFC3339),
			"user":         c.userSummary(),
			"methods":      openClawWSMethods,
		}, "")
	case "ping":
		return c.writeResponse(frame.ID, true, map[string]any{"pong": time.Now().UTC().Format(time.RFC3339)}, "")
	case "methods.list":
		return c.writeResponse(frame.ID, true, map[string]any{"methods": openClawWSMethods}, "")
	case "status", "status.get":
		if err := c.requirePermission("status.read"); err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, c.server.status(), "")
	case "control-plane.get", "control_plane.get":
		if err := c.requirePermission("status.read"); err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, c.server.controlPlaneSnapshot(), "")
	case "agents.list":
		if !HasPermission(c.user, "config.read") && !HasPermission(c.user, "config.write") {
			return fmt.Errorf("forbidden: missing config.read")
		}
		return c.writeResponse(frame.ID, true, c.server.listAgentViews(), "")
	case "agents.get":
		if !HasPermission(c.user, "config.read") && !HasPermission(c.user, "config.write") {
			return fmt.Errorf("forbidden: missing config.read")
		}
		name := mapString(frame.Params, "name")
		if name == "" {
			return fmt.Errorf("name parameter required")
		}
		agent, ok := c.server.getAgentView(name)
		if !ok {
			return fmt.Errorf("agent not found: %s", name)
		}
		return c.writeResponse(frame.ID, true, agent, "")
	case "providers.list":
		if !HasPermission(c.user, "config.read") && !HasPermission(c.user, "config.write") {
			return fmt.Errorf("forbidden: missing config.read")
		}
		return c.writeResponse(frame.ID, true, c.server.listProviderViews(), "")
	case "agent-bindings.list", "agent_bindings.list":
		if !HasPermission(c.user, "config.read") && !HasPermission(c.user, "config.write") {
			return fmt.Errorf("forbidden: missing config.read")
		}
		return c.writeResponse(frame.ID, true, c.server.listAgentBindingViews(), "")
	case "app-bindings.list", "app_bindings.list":
		if err := c.requirePermission("apps.read"); err != nil {
			return err
		}
		items, err := c.server.listAppBindingViews(mapString(frame.Params, "app"))
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, items, "")
	case "app-pairings.list", "app_pairings.list":
		if err := c.requirePermission("apps.read"); err != nil {
			return err
		}
		items, err := c.server.listAppPairingViews(mapString(frame.Params, "app"))
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, items, "")
	case "app-workflows.resolve", "app_workflows.resolve":
		if err := c.requirePermission("apps.read"); err != nil {
			return err
		}
		query := firstNonEmpty(mapString(frame.Params, "q"), mapString(frame.Params, "query"), mapString(frame.Params, "text"))
		if query == "" {
			return fmt.Errorf("q is required")
		}
		limit := mapInt(frame.Params, "limit", 3)
		return c.writeResponse(frame.ID, true, c.server.resolveAppWorkflowViews(ctx, query, limit), "")
	case "channels.list", "channels.status":
		if err := c.requirePermission("channels.read"); err != nil {
			return err
		}
		if c.server.channels == nil {
			return c.writeResponse(frame.ID, true, []any{}, "")
		}
		return c.writeResponse(frame.ID, true, c.server.channels.Statuses(), "")
	case "sessions.list":
		if err := c.requirePermission("sessions.read"); err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, map[string]any{"sessions": c.filteredSessions(frame.Params)}, "")
	case "sessions.get":
		if err := c.requirePermission("sessions.read"); err != nil {
			return err
		}
		result, err := c.server.wsSessionGet(c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "sessions.create", "sessions.spawn", "sessions_spawn":
		if err := c.requirePermission("sessions.write"); err != nil {
			return err
		}
		result, err := c.server.wsSessionCreate(c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "sessions.send", "sessions_send":
		if err := c.requirePermission("chat.send"); err != nil {
			return err
		}
		result, err := c.server.wsSessionSend(ctx, c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "sessions.history", "chat.history":
		if err := c.requirePermission("sessions.read"); err != nil {
			return err
		}
		result, err := c.server.wsSessionHistory(c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "sessions.patch":
		if err := c.requirePermission("sessions.write"); err != nil {
			return err
		}
		result, err := c.server.wsSessionPatch(c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "sessions.delete":
		if err := c.requirePermission("sessions.write"); err != nil {
			return err
		}
		result, err := c.server.wsSessionDelete(c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "sessions.abort", "sessions_abort", "chat.abort":
		if err := c.requirePermission("chat.send"); err != nil {
			return err
		}
		result, err := c.server.wsSessionAbort(c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "tasks.list":
		if err := c.requirePermission("tasks.read"); err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, c.filteredTasks(frame.Params), "")
	case "apps.list":
		if err := c.requirePermission("apps.read"); err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, c.server.listAppViews(), "")
	case "tools.list", "tools.catalog":
		if err := c.requirePermission("tools.read"); err != nil {
			return err
		}
		if c.server.app == nil || c.server.app.Agent == nil {
			return c.writeResponse(frame.ID, true, []any{}, "")
		}
		return c.writeResponse(frame.ID, true, c.server.app.Agent.ListTools(), "")
	case "plugins.list":
		if err := c.requirePermission("plugins.read"); err != nil {
			return err
		}
		if c.server.plugins == nil {
			return c.writeResponse(frame.ID, true, []any{}, "")
		}
		return c.writeResponse(frame.ID, true, c.server.plugins.List(), "")
	case "events.list":
		if err := c.requirePermission("events.read"); err != nil {
			return err
		}
		limit := mapInt(frame.Params, "limit", 24)
		return c.writeResponse(frame.ID, true, c.server.store.ListEvents(limit), "")
	case "events.subscribe":
		if err := c.requirePermission("events.read"); err != nil {
			return err
		}
		c.startEventStream()
		return c.writeResponse(frame.ID, true, map[string]any{"subscribed": true}, "")
	case "events.unsubscribe":
		if err := c.requirePermission("events.read"); err != nil {
			return err
		}
		c.stopEventStream()
		return c.writeResponse(frame.ID, true, map[string]any{"subscribed": false}, "")
	case "providers.update":
		if err := c.requirePermission("config.write"); err != nil {
			return err
		}
		if frame.Params == nil {
			return fmt.Errorf("params required")
		}
		providerData := frame.Params["provider"]
		if providerData == nil {
			return fmt.Errorf("provider data required")
		}
		providerJSON, err := json.Marshal(providerData)
		if err != nil {
			return fmt.Errorf("invalid provider data: %v", err)
		}
		var provider config.ProviderProfile
		if err := json.Unmarshal(providerJSON, &provider); err != nil {
			return fmt.Errorf("invalid provider format: %v", err)
		}
		// 调用 REST 处理逻辑
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/providers", bytes.NewReader(providerJSON))
		req = req.WithContext(ctx)
		c.server.handleProviders(w, req)
		if w.Code >= 400 {
			return fmt.Errorf("provider update failed: %s", w.Body.String())
		}
		var result providerView
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "providers.default":
		if err := c.requirePermission("config.write"); err != nil {
			return err
		}
		providerRef := mapString(frame.Params, "provider_ref")
		if providerRef == "" {
			return fmt.Errorf("provider_ref required")
		}
		reqBody, err := json.Marshal(map[string]string{"provider_ref": providerRef})
		if err != nil {
			return fmt.Errorf("failed to marshal request: %v", err)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/providers/default", bytes.NewReader(reqBody))
		req = req.WithContext(ctx)
		c.server.handleDefaultProvider(w, req)
		if w.Code >= 400 {
			return fmt.Errorf("default provider update failed: %s", w.Body.String())
		}
		var result providerView
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "providers.test":
		if !HasPermission(c.user, "config.write") && !HasPermission(c.user, "config.read") {
			return fmt.Errorf("forbidden: missing config.read")
		}
		if frame.Params == nil {
			return fmt.Errorf("params required")
		}
		providerData := frame.Params["provider"]
		if providerData == nil {
			return fmt.Errorf("provider data required")
		}
		providerJSON, err := json.Marshal(providerData)
		if err != nil {
			return fmt.Errorf("invalid provider data: %v", err)
		}
		// 调用 REST 处理逻辑
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/providers/test", bytes.NewReader(providerJSON))
		req = req.WithContext(ctx)
		c.server.handleProviderTest(w, req)
		if w.Code >= 400 {
			return fmt.Errorf("provider test failed: %s", w.Body.String())
		}
		var result providerHealth
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "agent-bindings.update", "agent_bindings.update":
		if err := c.requirePermission("config.write"); err != nil {
			return err
		}
		if frame.Params == nil {
			return fmt.Errorf("params required")
		}
		bindingData := frame.Params["binding"]
		if bindingData == nil {
			return fmt.Errorf("binding data required")
		}
		bindingJSON, err := json.Marshal(bindingData)
		if err != nil {
			return fmt.Errorf("invalid binding data: %v", err)
		}
		// 调用 REST 处理逻辑
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/agent-bindings", bytes.NewReader(bindingJSON))
		req = req.WithContext(ctx)
		c.server.handleAgentBindings(w, req)
		if w.Code >= 400 {
			return fmt.Errorf("agent binding update failed: %s", w.Body.String())
		}
		var result []agentBindingView
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "app-bindings.update", "app_bindings.update":
		if err := c.requirePermission("apps.write"); err != nil {
			return err
		}
		if frame.Params == nil {
			return fmt.Errorf("params required")
		}
		bindingData := frame.Params["binding"]
		if bindingData == nil {
			return fmt.Errorf("binding data required")
		}
		bindingJSON, err := json.Marshal(bindingData)
		if err != nil {
			return fmt.Errorf("invalid binding data: %v", err)
		}
		// 调用 REST 处理逻辑
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/app-bindings", bytes.NewReader(bindingJSON))
		req = req.WithContext(ctx)
		c.server.handleAppBindings(w, req)
		if w.Code >= 400 {
			return fmt.Errorf("app binding update failed: %s", w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "app-pairings.update", "app_pairings.update":
		if err := c.requirePermission("apps.write"); err != nil {
			return err
		}
		if frame.Params == nil {
			return fmt.Errorf("params required")
		}
		pairingData := frame.Params["pairing"]
		if pairingData == nil {
			return fmt.Errorf("pairing data required")
		}
		pairingJSON, err := json.Marshal(pairingData)
		if err != nil {
			return fmt.Errorf("invalid pairing data: %v", err)
		}
		// 调用 REST 处理逻辑
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/app-pairings", bytes.NewReader(pairingJSON))
		req = req.WithContext(ctx)
		c.server.handleAppPairings(w, req)
		if w.Code >= 400 {
			return fmt.Errorf("app pairing update failed: %s", w.Body.String())
		}
		var result map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			return fmt.Errorf("failed to parse response: %v", err)
		}
		return c.writeResponse(frame.ID, true, result, "")
	case "chat.send":
		if err := c.requirePermission("chat.send"); err != nil {
			return err
		}
		result, err := c.server.wsChatSend(ctx, c.user, frame.Params)
		if err != nil {
			return err
		}
		return c.writeResponse(frame.ID, true, result, "")

	case "device.pairing.generate":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		deviceName := mapString(frame.Params, "device_name")
		deviceType := mapString(frame.Params, "device_type")
		if deviceType == "" {
			deviceType = "cli"
		}
		code, err := c.server.devicePairing.GeneratePairingCode(deviceName, deviceType)
		if err != nil {
			return c.writeResponse(frame.ID, false, nil, err.Error())
		}
		return c.writeResponse(frame.ID, true, map[string]any{
			"code":    code.Code,
			"expires": code.ExpiresAt.Format(time.RFC3339),
			"device":  code.DeviceName,
			"type":    code.DeviceType,
		}, "")

	case "device.pairing.validate":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		code := mapString(frame.Params, "code")
		if code == "" {
			return c.writeResponse(frame.ID, false, nil, "code is required")
		}
		codeObj, err := c.server.devicePairing.ValidatePairingCode(code)
		if err != nil {
			return c.writeResponse(frame.ID, false, nil, err.Error())
		}
		return c.writeResponse(frame.ID, true, map[string]any{
			"valid":       true,
			"device_name": codeObj.DeviceName,
			"device_type": codeObj.DeviceType,
			"expires":     codeObj.ExpiresAt.Format(time.RFC3339),
		}, "")

	case "device.pairing.pair":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		code := mapString(frame.Params, "code")
		deviceID := mapString(frame.Params, "device_id")
		deviceName := mapString(frame.Params, "device_name")
		if code == "" || deviceID == "" {
			return c.writeResponse(frame.ID, false, nil, "code and device_id are required")
		}
		pairing, err := c.server.devicePairing.CompletePairing(code, deviceID)
		if err != nil {
			return c.writeResponse(frame.ID, false, nil, err.Error())
		}
		if deviceName != "" {
			pairing.DeviceName = deviceName
		}
		return c.writeResponse(frame.ID, true, pairing, "")

	case "device.pairing.unpair":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		deviceID := mapString(frame.Params, "device_id")
		if deviceID == "" {
			return c.writeResponse(frame.ID, false, nil, "device_id is required")
		}
		if err := c.server.devicePairing.Unpair(deviceID); err != nil {
			return c.writeResponse(frame.ID, false, nil, err.Error())
		}
		return c.writeResponse(frame.ID, true, map[string]any{"ok": true}, "")

	case "device.pairing.list":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		devices := c.server.devicePairing.ListPaired()
		return c.writeResponse(frame.ID, true, map[string]any{"devices": devices}, "")

	case "device.pairing.status":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		return c.writeResponse(frame.ID, true, c.server.devicePairing.GetStatus(), "")

	case "device.pairing.renew":
		if c.server.devicePairing == nil {
			return c.writeResponse(frame.ID, false, nil, "device pairing not initialized")
		}
		deviceID := mapString(frame.Params, "device_id")
		if deviceID == "" {
			return c.writeResponse(frame.ID, false, nil, "device_id is required")
		}
		pairing, err := c.server.devicePairing.RenewPairing(deviceID)
		if err != nil {
			return c.writeResponse(frame.ID, false, nil, err.Error())
		}
		return c.writeResponse(frame.ID, true, pairing, "")

	default:
		return fmt.Errorf("unsupported method: %s", method)
	}
}

func (c *openClawWSConn) userSummary() map[string]any {
	if c.user == nil {
		return map[string]any{"name": "", "role": ""}
	}
	return map[string]any{
		"name":        c.user.Name,
		"role":        c.user.Role,
		"permissions": c.user.Permissions,
	}
}

func (c *openClawWSConn) filteredSessions(params map[string]any) []*Session {
	workspace := mapString(params, "workspace")
	items := c.server.store.ListSessions()
	filtered := make([]*Session, 0, len(items))
	for _, session := range items {
		if workspace != "" && session.Workspace != workspace {
			continue
		}
		if c.server.canAccessSession(c.user, session) {
			filtered = append(filtered, session)
		}
	}
	return filtered
}

func (c *openClawWSConn) filteredTasks(params map[string]any) []*Task {
	workspace := mapString(params, "workspace")
	status := mapString(params, "status")
	items := c.server.store.ListTasks()
	filtered := make([]*Task, 0, len(items))
	for _, task := range items {
		if workspace != "" && task.Workspace != workspace {
			continue
		}
		if status != "" && !strings.EqualFold(task.Status, status) {
			continue
		}
		filtered = append(filtered, task)
	}
	return filtered
}

func (c *openClawWSConn) startEventStream() {
	if c.eventStream != nil || c.server.bus == nil {
		return
	}
	ch := c.server.bus.Subscribe(32)
	c.eventStream = ch
	go func() {
		for {
			select {
			case <-c.closed:
				return
			case event, ok := <-ch:
				if !ok {
					return
				}
				if !c.canSeeEvent(event) {
					continue
				}
				if err := c.writeFrame(openClawWSFrame{
					Type:  "event",
					Event: "events.updated",
					Data:  event,
				}); err != nil {
					c.shutdown()
					return
				}
			}
		}
	}()
}

func (c *openClawWSConn) stopEventStream() {
	if c.eventStream == nil || c.server.bus == nil {
		return
	}
	c.server.bus.Unsubscribe(c.eventStream)
	c.eventStream = nil
}

func (c *openClawWSConn) canSeeEvent(event *Event) bool {
	return c.server.canUserSeeEvent(c.user, event)
}

func (c *openClawWSConn) requirePermission(permission string) error {
	if !HasPermission(c.user, permission) {
		return fmt.Errorf("forbidden: missing %s", permission)
	}
	return nil
}

func (c *openClawWSConn) requireAnyPermission(permissions ...string) error {
	for _, permission := range permissions {
		if HasPermission(c.user, permission) {
			return nil
		}
	}
	return fmt.Errorf("forbidden: missing one of %s", strings.Join(permissions, ", "))
}

func (c *openClawWSConn) writeResponse(id string, ok bool, data any, errMsg string) error {
	frame := openClawWSFrame{
		Type: "res",
		ID:   id,
		OK:   ok,
		Data: data,
	}
	if strings.TrimSpace(errMsg) != "" {
		frame.Error = strings.TrimSpace(errMsg)
	}
	return c.writeFrame(frame)
}

func (c *openClawWSConn) writeError(id string, errMsg string) error {
	return c.writeResponse(id, false, nil, errMsg)
}

func (c *openClawWSConn) writeFrame(frame openClawWSFrame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(frame)
}

func (c *openClawWSConn) shutdown() {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.stopEventStream()
		_ = c.conn.Close()
	})
}

func (s *Server) wsChatSend(ctx context.Context, user *AuthUser, params map[string]any) (map[string]any, error) {
	message := mapString(params, "message")
	if message == "" {
		return nil, fmt.Errorf("message is required")
	}
	title := mapString(params, "title")
	sessionID := sessionIDParam(params)
	if sessionID == "" {
		assistantName, err := s.resolveAgentName(firstNonEmpty(mapString(params, "agent"), mapString(params, "assistant")))
		if err != nil {
			return nil, err
		}
		orgID := mapString(params, "org")
		projectID := mapString(params, "project")
		workspaceID := firstNonEmpty(mapString(params, "workspace"), mapString(params, "workspace_id"))
		if workspaceID == "" {
			orgID, projectID, workspaceID = defaultResourceIDs(s.app.WorkingDir)
		}
		org, project, workspace, err := s.validateResourceSelection(orgID, projectID, workspaceID)
		if err != nil {
			return nil, err
		}
		if !HasHierarchyAccess(user, org.ID, project.ID, workspace.ID) {
			return nil, fmt.Errorf("forbidden")
		}
		session, err := s.sessions.CreateWithOptions(SessionCreateOptions{
			Title:       title,
			AgentName:   assistantName,
			Org:         org.ID,
			Project:     project.ID,
			Workspace:   workspace.ID,
			SessionMode: "main",
			QueueMode:   "fifo",
		})
		if err != nil {
			return nil, err
		}
		sessionID = session.ID
		s.appendEvent("session.created", session.ID, map[string]any{"title": session.Title, "org": session.Org, "project": session.Project, "workspace": session.Workspace})
	} else {
		session, ok := s.sessions.Get(sessionID)
		if !ok {
			return nil, fmt.Errorf("session not found: %s", sessionID)
		}
		if !s.canAccessSession(user, session) {
			return nil, fmt.Errorf("forbidden")
		}
	}
	response, updatedSession, err := s.runSessionMessage(ctx, sessionID, title, message)
	if err != nil {
		if errors.Is(err, ErrTaskWaitingApproval) {
			s.appendAudit(user, "chat.send", sessionID, map[string]any{"message_length": len(message), "transport": "ws", "status": "waiting_approval"})
			return s.sessionApprovalResponse(sessionID), nil
		}
		return nil, err
	}
	s.appendAudit(user, "chat.send", updatedSession.ID, map[string]any{"message_length": len(message), "transport": "ws"})
	return map[string]any{
		"response":    response,
		"session":     updatedSession,
		"session_key": updatedSession.ID,
	}, nil
}

func (s *Server) wsSessionCreate(user *AuthUser, params map[string]any) (map[string]any, error) {
	assistantName, err := s.resolveAgentName(firstNonEmpty(mapString(params, "agent"), mapString(params, "assistant")))
	if err != nil {
		return nil, err
	}
	orgID := mapString(params, "org")
	projectID := mapString(params, "project")
	workspaceID := firstNonEmpty(mapString(params, "workspace"), mapString(params, "workspace_id"))
	if workspaceID == "" {
		orgID, projectID, workspaceID = defaultResourceIDs(s.app.WorkingDir)
	}
	org, project, workspace, err := s.validateResourceSelection(orgID, projectID, workspaceID)
	if err != nil {
		return nil, err
	}
	if !HasHierarchyAccess(user, org.ID, project.ID, workspace.ID) {
		return nil, fmt.Errorf("forbidden")
	}

	var replyBack *bool
	if value, ok := mapOptionalBool(params, "reply_back"); ok {
		replyBack = &value
	}
	session, err := s.sessions.CreateWithOptions(SessionCreateOptions{
		Title:       mapString(params, "title"),
		AgentName:   assistantName,
		Org:         org.ID,
		Project:     project.ID,
		Workspace:   workspace.ID,
		SessionMode: mapString(params, "session_mode"),
		QueueMode:   mapString(params, "queue_mode"),
		ReplyBack:   replyBack != nil && *replyBack,
	})
	if err != nil {
		return nil, err
	}
	s.appendEvent("session.created", session.ID, map[string]any{
		"title":     session.Title,
		"org":       session.Org,
		"project":   session.Project,
		"workspace": session.Workspace,
		"source":    "ws.sessions.create",
	})
	return map[string]any{"session": session, "session_key": session.ID}, nil
}

func (s *Server) wsSessionSend(ctx context.Context, user *AuthUser, params map[string]any) (map[string]any, error) {
	session, err := s.wsSessionForUser(user, params)
	if err != nil {
		return nil, err
	}
	params = cloneAnyMap(params)
	params["session_id"] = session.ID
	return s.wsChatSend(ctx, user, params)
}

func (s *Server) wsSessionGet(user *AuthUser, params map[string]any) (map[string]any, error) {
	session, err := s.wsSessionForUser(user, params)
	if err != nil {
		return nil, err
	}
	return map[string]any{"session": session, "session_key": session.ID}, nil
}

func (s *Server) wsSessionHistory(user *AuthUser, params map[string]any) (map[string]any, error) {
	session, err := s.wsSessionForUser(user, params)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"session":     session,
		"session_key": session.ID,
		"history":     session.Messages,
	}, nil
}

func (s *Server) wsSessionPatch(user *AuthUser, params map[string]any) (map[string]any, error) {
	current, err := s.wsSessionForUser(user, params)
	if err != nil {
		return nil, err
	}
	sessionID := current.ID

	agentName := ""
	if requested := firstNonEmpty(mapString(params, "agent"), mapString(params, "assistant")); requested != "" {
		resolved, err := s.resolveAgentName(requested)
		if err != nil {
			return nil, err
		}
		agentName = resolved
	}

	orgID := firstNonEmpty(mapString(params, "org"), current.Org)
	projectID := firstNonEmpty(mapString(params, "project"), current.Project)
	workspaceID := firstNonEmpty(mapString(params, "workspace"), mapString(params, "workspace_id"), current.Workspace)
	org, project, workspace, err := s.validateResourceSelection(orgID, projectID, workspaceID)
	if err != nil {
		return nil, err
	}
	if !HasHierarchyAccess(user, org.ID, project.ID, workspace.ID) {
		return nil, fmt.Errorf("forbidden")
	}

	var replyBack *bool
	if value, ok := mapOptionalBool(params, "reply_back"); ok {
		replyBack = &value
	}
	session, err := s.sessions.PatchSession(sessionID, SessionPatchOptions{
		Title:       mapString(params, "title"),
		AgentName:   agentName,
		Org:         org.ID,
		Project:     project.ID,
		Workspace:   workspace.ID,
		SessionMode: mapString(params, "session_mode"),
		QueueMode:   mapString(params, "queue_mode"),
		ReplyBack:   replyBack,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"session": session, "session_key": session.ID}, nil
}

func (s *Server) wsSessionDelete(user *AuthUser, params map[string]any) (map[string]any, error) {
	session, err := s.wsSessionForUser(user, params)
	if err != nil {
		return nil, err
	}
	if err := s.sessions.Delete(session.ID); err != nil {
		return nil, err
	}
	s.appendEvent("session.deleted", session.ID, map[string]any{"source": "ws.sessions.delete"})
	return map[string]any{"deleted": true, "session_id": session.ID, "session_key": session.ID}, nil
}

func (s *Server) wsSessionAbort(user *AuthUser, params map[string]any) (map[string]any, error) {
	session, err := s.wsSessionForUser(user, params)
	if err != nil {
		return nil, err
	}
	active := s.abortActiveSessionRun(session.ID)
	session, err = s.sessions.Abort(session.ID)
	if err != nil {
		return nil, err
	}
	s.appendEvent("session.aborted", session.ID, map[string]any{"source": "ws.sessions.abort", "active_run": active})
	return map[string]any{"aborted": true, "session": session, "session_key": session.ID}, nil
}

func mapString(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func mapInt(values map[string]any, key string, fallback int) int {
	if values == nil {
		return fallback
	}
	switch value := values[key].(type) {
	case float64:
		if int(value) > 0 {
			return int(value)
		}
	case int:
		if value > 0 {
			return value
		}
	}
	return fallback
}

func mapOptionalBool(values map[string]any, key string) (bool, bool) {
	if values == nil {
		return false, false
	}
	value, ok := values[key]
	if !ok {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
	}
	return false, false
}

func sessionIDParam(values map[string]any) string {
	return firstNonEmpty(
		mapString(values, "session_id"),
		mapString(values, "session_key"),
		mapString(values, "sessionKey"),
		mapString(values, "id"),
		mapString(values, "session"),
	)
}

func (s *Server) wsSessionForUser(user *AuthUser, params map[string]any) (*Session, error) {
	sessionID := sessionIDParam(params)
	if sessionID == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	session, ok := s.sessions.Get(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if !s.canAccessSession(user, session) {
		return nil, fmt.Errorf("forbidden")
	}
	return session, nil
}
