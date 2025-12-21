# Beads Orchestra: Implementation Plan
## Workflow Orchestration for AI Agent Coordination

**Status:** Design Phase
**Created:** 2025-12-19
**Epic ID:** TBD (create as `bd-orchestra` after approval)

---

## Executive Summary

This document outlines a detailed implementation plan for **Beads Orchestra**, a workflow orchestration layer that transforms beads from a passive issue tracker into an active AI agent coordinator. This feature leverages existing architectural strengths (custom tables, daemon architecture, dependency graph, RPC protocol) while adding automatic work dispatch, execution state tracking, and agent coordination capabilities.

### Value Proposition

**Before Orchestra:**
- Agents manually poll `bd ready` for available work
- Agents implement their own retry logic and error handling
- No automatic work distribution across multiple agents
- Manual coordination required for parallel workflows
- No execution history or observability

**After Orchestra:**
- Automatic work dispatch when dependencies resolve
- Built-in retry logic and timeout handling
- Intelligent load balancing across agent capabilities
- Parallel execution of independent tasks
- Full execution audit trail and observability

---

## Architecture Overview

### Core Design Principles

1. **Extension Pattern**: Use existing `UnderlyingDB()` access for custom tables (documented in EXTENDING.md)
2. **Daemon Integration**: Leverage existing daemon infrastructure for long-running orchestration
3. **Backward Compatibility**: Orchestration is opt-in; existing workflows remain unchanged
4. **RPC-Native**: Add orchestration operations to existing RPC protocol
5. **SQLite-First**: Maintain beads' philosophy of local-first, git-backed storage

### System Components

```
┌─────────────────────────────────────────────────────────────────┐
│                    Beads Orchestra Stack                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ CLI Layer (cmd/bd/)                                       │ │
│  │  - bd orchestrate start/stop                              │ │
│  │  - bd agent register/unregister                           │ │
│  │  - bd executions list/retry/cancel                        │ │
│  └───────────────────────────────────────────────────────────┘ │
│                              │                                  │
│                              ▼                                  │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ RPC Protocol (internal/rpc/protocol.go)                   │ │
│  │  + OpOrchestrate, OpAgentRegister, OpExecutionList        │ │
│  └───────────────────────────────────────────────────────────┘ │
│                              │                                  │
│                              ▼                                  │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ Orchestration Engine (internal/orchestration/)            │ │
│  │  - Workflow Engine (engine.go)                            │ │
│  │  - Agent Registry (registry.go)                           │ │
│  │  - Execution Manager (executions.go)                      │ │
│  │  - Dispatcher (dispatcher.go)                             │ │
│  └───────────────────────────────────────────────────────────┘ │
│                              │                                  │
│                              ▼                                  │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ Custom Tables (.beads/beads.db)                           │ │
│  │  - orchestration_executions                               │ │
│  │  - orchestration_agents                                   │ │
│  │  - orchestration_workflows (future)                       │ │
│  └───────────────────────────────────────────────────────────┘ │
│                              │                                  │
│                              ▼                                  │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ Core Storage (internal/storage/sqlite/)                   │ │
│  │  - Existing issue/dependency/event tables                 │ │
│  │  - GetReadyWork() for finding unblocked tasks             │ │
│  └───────────────────────────────────────────────────────────┘ │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

---

## Database Schema Design

### Table: `orchestration_executions`

Tracks individual work item executions (one execution per issue assignment).

```sql
CREATE TABLE orchestration_executions (
    -- Identity
    id TEXT PRIMARY KEY,                    -- exec-{hash} format
    issue_id TEXT NOT NULL,                 -- FK to issues.id
    workflow_id TEXT,                       -- Optional: parent workflow (future)

    -- Agent assignment
    agent_id TEXT,                          -- FK to orchestration_agents.id
    assigned_at INTEGER,                    -- Unix timestamp (ms)

    -- Execution lifecycle
    status TEXT NOT NULL,                   -- queued, running, completed, failed, cancelled, timeout
    started_at INTEGER,                     -- Unix timestamp (ms)
    completed_at INTEGER,                   -- Unix timestamp (ms)
    timeout_at INTEGER,                     -- Unix timestamp (ms), deadline for execution

    -- Retry logic
    retry_count INTEGER DEFAULT 0,          -- Number of retries attempted
    max_retries INTEGER DEFAULT 3,          -- Maximum retries before giving up
    last_error TEXT,                        -- Error message from last failure

    -- Metadata
    created_at INTEGER NOT NULL,            -- Unix timestamp (ms)
    updated_at INTEGER NOT NULL,            -- Unix timestamp (ms)
    created_by TEXT,                        -- Who/what created the execution

    -- Result data (optional)
    result_data TEXT,                       -- JSON blob for execution results

    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES orchestration_agents(id) ON DELETE SET NULL
);

-- Indexes for performance
CREATE INDEX idx_orchestration_executions_issue ON orchestration_executions(issue_id);
CREATE INDEX idx_orchestration_executions_agent ON orchestration_executions(agent_id);
CREATE INDEX idx_orchestration_executions_status ON orchestration_executions(status);
CREATE INDEX idx_orchestration_executions_timeout ON orchestration_executions(timeout_at) WHERE status = 'running';
```

**Status Transitions:**
```
queued → running → completed
             ↓
           failed → queued (retry) → failed (max retries)
             ↓
         cancelled
             ↓
          timeout
```

### Table: `orchestration_agents`

Registry of available agents and their capabilities.

```sql
CREATE TABLE orchestration_agents (
    -- Identity
    id TEXT PRIMARY KEY,                    -- Agent identifier (e.g., "roughneck-1", "db-specialist")

    -- Capabilities (JSON array of strings)
    capabilities TEXT NOT NULL,             -- JSON: ["go", "python", "testing", "database"]

    -- Agent status
    status TEXT NOT NULL,                   -- idle, busy, offline, paused
    current_task TEXT,                      -- FK to orchestration_executions.id (if busy)

    -- Lifecycle
    last_heartbeat INTEGER NOT NULL,        -- Unix timestamp (ms), for liveness detection
    registered_at INTEGER NOT NULL,         -- Unix timestamp (ms)
    last_seen INTEGER,                      -- Unix timestamp (ms)

    -- Configuration
    max_concurrent_tasks INTEGER DEFAULT 1, -- How many tasks this agent can handle
    priority INTEGER DEFAULT 0,             -- Agent priority (higher = preferred)

    -- Metadata
    metadata TEXT,                          -- JSON blob for agent-specific config

    FOREIGN KEY (current_task) REFERENCES orchestration_executions(id) ON DELETE SET NULL
);

-- Indexes
CREATE INDEX idx_orchestration_agents_status ON orchestration_agents(status);
CREATE INDEX idx_orchestration_agents_heartbeat ON orchestration_agents(last_heartbeat);
```

**Status Meanings:**
- `idle`: Agent is available for work
- `busy`: Agent is currently executing a task
- `offline`: No heartbeat received within threshold (60s default)
- `paused`: Agent temporarily disabled (manual intervention)

### Table: `orchestration_workflows` (Future Phase)

For complex multi-issue workflows with conditional logic.

```sql
-- Phase 2+ feature - not in initial implementation
CREATE TABLE orchestration_workflows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    definition TEXT NOT NULL,               -- JSON workflow DSL
    status TEXT NOT NULL,                   -- pending, running, completed, failed
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
```

---

## Component Design

### 1. Orchestration Engine (`internal/orchestration/engine.go`)

**Responsibilities:**
- Monitor `bd ready` for unblocked work
- Match tasks to available agents by capability
- Create execution records
- Dispatch work to agents
- Monitor execution timeouts
- Handle retry logic

**Core Loop:**
```go
type Engine struct {
    store       storage.Storage
    db          *sql.DB
    agents      *AgentRegistry
    dispatcher  *Dispatcher
    execManager *ExecutionManager
    ticker      *time.Ticker
    ctx         context.Context
    cancel      context.CancelFunc
}

func (e *Engine) Run() error {
    for {
        select {
        case <-e.ticker.C:
            // 1. Find ready work
            readyIssues := e.getReadyWork()

            // 2. Match to capable idle agents
            for _, issue := range readyIssues {
                agent := e.agents.FindCapable(issue)
                if agent != nil {
                    e.dispatcher.Assign(issue, agent)
                }
            }

            // 3. Check for timeouts
            e.execManager.CheckTimeouts()

            // 4. Process retries
            e.execManager.ProcessRetries()

        case <-e.ctx.Done():
            return nil
        }
    }
}
```

**Configuration:**
```go
type EngineConfig struct {
    TickInterval     time.Duration // How often to check for work (default: 5s)
    DefaultTimeout   time.Duration // Default task timeout (default: 30m)
    MaxRetries       int           // Default max retries (default: 3)
    HeartbeatTimeout time.Duration // Agent offline threshold (default: 60s)
}
```

### 2. Agent Registry (`internal/orchestration/registry.go`)

**Responsibilities:**
- Track registered agents
- Monitor agent health (heartbeat)
- Match agents to tasks by capability
- Load balancing

**Key Methods:**
```go
type AgentRegistry struct {
    db *sql.DB
    mu sync.RWMutex
}

// Register adds or updates an agent
func (r *AgentRegistry) Register(agent *Agent) error

// Heartbeat updates agent last_seen timestamp
func (r *AgentRegistry) Heartbeat(agentID string) error

// FindCapable finds idle agent with required capabilities
// Returns nil if no capable agent available
func (r *AgentRegistry) FindCapable(issue *types.Issue) (*Agent, error)

// MarkBusy transitions agent to busy state
func (r *AgentRegistry) MarkBusy(agentID string, executionID string) error

// MarkIdle transitions agent back to idle
func (r *AgentRegistry) MarkIdle(agentID string) error

// MarkOffline marks agents with stale heartbeats as offline
func (r *AgentRegistry) MarkOffline() error
```

**Capability Matching Logic:**
```go
// Extract required capabilities from issue labels
// Example: issue with labels ["requires:go", "requires:testing"]
//          matches agent with capabilities ["go", "testing", "python"]

func extractRequiredCapabilities(issue *types.Issue) []string {
    var required []string
    for _, label := range issue.Labels {
        if strings.HasPrefix(label, "requires:") {
            cap := strings.TrimPrefix(label, "requires:")
            required = append(required, cap)
        }
    }
    return required
}

func agentHasCapabilities(agent *Agent, required []string) bool {
    capabilities := parseCapabilities(agent.Capabilities)
    capMap := make(map[string]bool)
    for _, c := range capabilities {
        capMap[c] = true
    }

    for _, r := range required {
        if !capMap[r] {
            return false
        }
    }
    return true
}
```

### 3. Execution Manager (`internal/orchestration/executions.go`)

**Responsibilities:**
- Create/update execution records
- Track execution state
- Timeout detection
- Retry logic

**Key Methods:**
```go
type ExecutionManager struct {
    db *sql.DB
}

// CreateExecution creates new execution record
func (m *ExecutionManager) CreateExecution(exec *Execution) error

// GetExecution retrieves execution by ID
func (m *ExecutionManager) GetExecution(id string) (*Execution, error)

// UpdateStatus updates execution status
func (m *ExecutionManager) UpdateStatus(id string, status Status, err error) error

// CheckTimeouts finds running executions past timeout_at
func (m *ExecutionManager) CheckTimeouts() ([]*Execution, error)

// ProcessRetries finds failed executions eligible for retry
func (m *ExecutionManager) ProcessRetries() ([]*Execution, error)

// ListExecutions queries executions with filters
func (m *ExecutionManager) ListExecutions(filter ExecutionFilter) ([]*Execution, error)
```

**Timeout Handling:**
```go
func (m *ExecutionManager) CheckTimeouts() ([]*Execution, error) {
    now := time.Now().UnixMilli()

    // Find executions past deadline
    rows, err := m.db.Query(`
        SELECT id, issue_id, agent_id, retry_count, max_retries
        FROM orchestration_executions
        WHERE status = 'running' AND timeout_at <= ?
    `, now)

    var timedOut []*Execution
    for rows.Next() {
        exec := scanExecution(rows)

        // Transition to timeout status
        m.UpdateStatus(exec.ID, StatusTimeout,
            fmt.Errorf("execution exceeded timeout"))

        // If retries available, queue for retry
        if exec.RetryCount < exec.MaxRetries {
            m.queueRetry(exec)
        }

        timedOut = append(timedOut, exec)
    }

    return timedOut, nil
}
```

### 4. Dispatcher (`internal/orchestration/dispatcher.go`)

**Responsibilities:**
- Assign work to agents
- Invoke agent execution (via MCP, RPC, or custom protocol)
- Handle agent responses

**Key Methods:**
```go
type Dispatcher struct {
    db          *sql.DB
    agents      *AgentRegistry
    execManager *ExecutionManager
    protocol    DispatchProtocol // How to communicate with agents
}

// Assign creates execution and dispatches to agent
func (d *Dispatcher) Assign(issue *types.Issue, agent *Agent) error

// Protocol interface for different agent communication methods
type DispatchProtocol interface {
    Send(agent *Agent, issue *types.Issue, execution *Execution) error
    Receive(execution *Execution) (*Result, error)
}
```

**Dispatch Flow:**
```go
func (d *Dispatcher) Assign(issue *types.Issue, agent *Agent) error {
    // 1. Create execution record
    exec := &Execution{
        ID:         generateExecutionID(),
        IssueID:    issue.ID,
        AgentID:    agent.ID,
        Status:     StatusQueued,
        CreatedAt:  time.Now(),
        TimeoutAt:  time.Now().Add(30 * time.Minute),
        MaxRetries: 3,
    }

    if err := d.execManager.CreateExecution(exec); err != nil {
        return err
    }

    // 2. Mark agent as busy
    if err := d.agents.MarkBusy(agent.ID, exec.ID); err != nil {
        return err
    }

    // 3. Send work to agent
    exec.Status = StatusRunning
    exec.StartedAt = time.Now()
    d.execManager.UpdateStatus(exec.ID, StatusRunning, nil)

    // 4. Dispatch via protocol (async)
    go func() {
        err := d.protocol.Send(agent, issue, exec)
        if err != nil {
            d.execManager.UpdateStatus(exec.ID, StatusFailed, err)
            d.agents.MarkIdle(agent.ID)
        }
    }()

    return nil
}
```

---

## CLI Commands

### `bd orchestrate` - Orchestration Control

**Start Orchestration:**
```bash
bd orchestrate start [flags]

Flags:
  --interval duration     Check interval for ready work (default: 5s)
  --timeout duration      Default task timeout (default: 30m)
  --max-retries int       Max retries for failed tasks (default: 3)
  --heartbeat-timeout     Agent offline threshold (default: 60s)
```

**Stop Orchestration:**
```bash
bd orchestrate stop
```

**Status:**
```bash
bd orchestrate status

Output:
  Status: running
  Started: 2025-12-19 10:30:45
  Agents: 3 registered (2 idle, 1 busy)
  Pending: 5 issues
  Running: 2 executions
  Completed: 47 executions
```

### `bd agent` - Agent Management

**Register Agent:**
```bash
bd agent register [flags]

Flags:
  --id string             Agent identifier (required)
  --capabilities strings  Comma-separated capabilities (required)
  --priority int          Agent priority (default: 0)
  --max-tasks int         Max concurrent tasks (default: 1)

Example:
  bd agent register --id=roughneck-1 --capabilities=go,testing --priority=10
```

**Unregister Agent:**
```bash
bd agent unregister <agent-id>
```

**List Agents:**
```bash
bd agent list [--json]

Output:
  ID            STATUS  CAPABILITIES       CURRENT TASK  LAST HEARTBEAT
  roughneck-1   busy    go, testing        exec-a3f8     2s ago
  roughneck-2   idle    python, research   -             5s ago
  db-spec       idle    database, sql      -             3s ago
```

**Heartbeat (for agent implementations):**
```bash
bd agent heartbeat <agent-id>
```

### `bd executions` - Execution Management

**List Executions:**
```bash
bd executions list [flags]

Flags:
  --status string    Filter by status (queued/running/completed/failed/timeout/cancelled)
  --agent string     Filter by agent ID
  --issue string     Filter by issue ID
  --limit int        Max results (default: 50)
  --json             Output JSON

Output:
  ID         ISSUE      AGENT        STATUS     STARTED         COMPLETED
  exec-a3f8  bd-x9k2    roughneck-1  running    2m ago          -
  exec-b7n4  bd-m3p1    roughneck-2  completed  15m ago         10m ago
  exec-c2k9  bd-j8f3    db-spec      failed     1h ago          1h ago
```

**Show Execution:**
```bash
bd executions show <execution-id>

Output:
  Execution: exec-a3f8
  Issue: bd-x9k2 (Implement authentication)
  Agent: roughneck-1
  Status: running
  Started: 2025-12-19 10:32:15
  Timeout: 2025-12-19 11:02:15
  Retry Count: 0 / 3

  Timeline:
    2025-12-19 10:30:00  Created
    2025-12-19 10:32:15  Started by roughneck-1
```

**Retry Execution:**
```bash
bd executions retry <execution-id>
```

**Cancel Execution:**
```bash
bd executions cancel <execution-id>
```

---

## RPC Protocol Extensions

### New Operations

Add to `internal/rpc/protocol.go`:

```go
const (
    // ... existing ops ...

    // Orchestration operations
    OpOrchestrateStart   = "orchestrate_start"
    OpOrchestrateStop    = "orchestrate_stop"
    OpOrchestrateStatus  = "orchestrate_status"

    // Agent operations
    OpAgentRegister      = "agent_register"
    OpAgentUnregister    = "agent_unregister"
    OpAgentList          = "agent_list"
    OpAgentHeartbeat     = "agent_heartbeat"

    // Execution operations
    OpExecutionCreate    = "execution_create"
    OpExecutionUpdate    = "execution_update"
    OpExecutionList      = "execution_list"
    OpExecutionShow      = "execution_show"
    OpExecutionRetry     = "execution_retry"
    OpExecutionCancel    = "execution_cancel"
)

// OrchestrateStartArgs represents arguments for starting orchestration
type OrchestrateStartArgs struct {
    Interval         string `json:"interval"`           // e.g., "5s"
    DefaultTimeout   string `json:"default_timeout"`    // e.g., "30m"
    MaxRetries       int    `json:"max_retries"`
    HeartbeatTimeout string `json:"heartbeat_timeout"`  // e.g., "60s"
}

// AgentRegisterArgs represents arguments for agent registration
type AgentRegisterArgs struct {
    ID           string   `json:"id"`
    Capabilities []string `json:"capabilities"`
    Priority     int      `json:"priority,omitempty"`
    MaxTasks     int      `json:"max_tasks,omitempty"`
    Metadata     string   `json:"metadata,omitempty"` // JSON blob
}

// ExecutionListArgs represents arguments for listing executions
type ExecutionListArgs struct {
    Status  string `json:"status,omitempty"`
    AgentID string `json:"agent_id,omitempty"`
    IssueID string `json:"issue_id,omitempty"`
    Limit   int    `json:"limit,omitempty"`
}

// ExecutionUpdateArgs for agent progress updates
type ExecutionUpdateArgs struct {
    ID         string `json:"id"`
    Status     string `json:"status,omitempty"`
    Error      string `json:"error,omitempty"`
    ResultData string `json:"result_data,omitempty"` // JSON blob
}
```

### RPC Server Handlers

Add to daemon RPC server (`internal/rpc/server_orchestration.go`):

```go
func (s *Server) handleOrchestrateStart(args *OrchestrateStartArgs) (*Response, error) {
    // Parse config
    interval, _ := time.ParseDuration(args.Interval)
    timeout, _ := time.ParseDuration(args.DefaultTimeout)

    // Start orchestration engine
    config := &orchestration.EngineConfig{
        TickInterval:   interval,
        DefaultTimeout: timeout,
        MaxRetries:     args.MaxRetries,
    }

    if err := s.orchestrationEngine.Start(config); err != nil {
        return errorResponse(err), nil
    }

    return successResponse(map[string]string{"status": "started"}), nil
}

func (s *Server) handleAgentRegister(args *AgentRegisterArgs) (*Response, error) {
    agent := &orchestration.Agent{
        ID:           args.ID,
        Capabilities: strings.Join(args.Capabilities, ","),
        Priority:     args.Priority,
        MaxTasks:     args.MaxTasks,
        Status:       "idle",
        RegisteredAt: time.Now(),
    }

    if err := s.orchestrationEngine.Registry().Register(agent); err != nil {
        return errorResponse(err), nil
    }

    return successResponse(agent), nil
}

func (s *Server) handleExecutionUpdate(args *ExecutionUpdateArgs) (*Response, error) {
    var err error
    if args.Error != "" {
        err = fmt.Errorf("%s", args.Error)
    }

    status := orchestration.ParseStatus(args.Status)
    if err := s.orchestrationEngine.Executions().UpdateStatus(
        args.ID, status, err); err != nil {
        return errorResponse(err), nil
    }

    return successResponse(map[string]string{"status": "updated"}), nil
}
```

---

## Phased Implementation Roadmap

### Phase 0: Foundation (Week 1)

**Goal:** Set up infrastructure without breaking changes

**Tasks:**
1. Create `internal/orchestration/` package structure
2. Define schema in `internal/orchestration/schema.go`
3. Implement database initialization (custom tables via `UnderlyingDB()`)
4. Add migration logic to create tables on first use
5. Write comprehensive tests for schema

**Deliverables:**
- [ ] Package skeleton
- [ ] Schema definitions
- [ ] Table creation logic
- [ ] Unit tests
- [ ] Documentation in `docs/orchestration.md`

**Files to Create:**
```
internal/orchestration/
├── schema.go           # Table definitions
├── schema_test.go      # Schema tests
├── types.go            # Execution, Agent, Workflow types
└── README.md           # Package overview
```

**Example Code:**
```go
// internal/orchestration/schema.go
package orchestration

const SchemaVersion = 1

const createExecutionsTable = `
CREATE TABLE IF NOT EXISTS orchestration_executions (
    id TEXT PRIMARY KEY,
    issue_id TEXT NOT NULL,
    -- ... rest of schema ...
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE
);
`

func InitializeSchema(db *sql.DB) error {
    // Check if already initialized
    var count int
    err := db.QueryRow(`
        SELECT COUNT(*) FROM sqlite_master
        WHERE type='table' AND name='orchestration_executions'
    `).Scan(&count)

    if err != nil {
        return err
    }

    if count > 0 {
        return nil // Already initialized
    }

    // Create tables
    _, err = db.Exec(createExecutionsTable)
    if err != nil {
        return fmt.Errorf("failed to create executions table: %w", err)
    }

    // ... create other tables ...

    return nil
}
```

### Phase 1: Core Engine (Week 2-3)

**Goal:** Implement basic orchestration loop

**Tasks:**
1. Implement `ExecutionManager` (CRUD for executions)
2. Implement `AgentRegistry` (agent lifecycle management)
3. Implement basic `Engine` (polling loop for ready work)
4. Add timeout detection logic
5. Add retry logic
6. Integration tests with mock agents

**Deliverables:**
- [ ] ExecutionManager with full CRUD
- [ ] AgentRegistry with capability matching
- [ ] Engine with basic dispatch loop
- [ ] Timeout/retry handling
- [ ] Integration tests

**Files to Create:**
```
internal/orchestration/
├── executions.go       # ExecutionManager
├── executions_test.go
├── registry.go         # AgentRegistry
├── registry_test.go
├── engine.go           # Orchestration engine
├── engine_test.go
└── matcher.go          # Capability matching logic
```

**Key Test Cases:**
```go
// executions_test.go
func TestExecutionLifecycle(t *testing.T) {
    // Test: queued → running → completed
    // Test: running → timeout → retry → completed
    // Test: running → failed → retry → failed (max retries)
}

// registry_test.go
func TestCapabilityMatching(t *testing.T) {
    // Test: agent with ["go", "testing"] matches issue with "requires:go"
    // Test: agent with ["python"] does NOT match issue with "requires:go"
    // Test: multiple matching agents returns highest priority
}

// engine_test.go
func TestOrchestratorLoop(t *testing.T) {
    // Create issue with dependencies
    // Mark blocker as closed
    // Verify dependent issue gets dispatched
}
```

### Phase 2: CLI Integration (Week 3-4)

**Goal:** Add user-facing commands

**Tasks:**
1. Create `cmd/bd/orchestrate.go` command
2. Create `cmd/bd/agent.go` command
3. Create `cmd/bd/executions.go` command
4. Add RPC protocol definitions
5. Implement RPC handlers in daemon
6. Add JSON output support
7. Write CLI integration tests

**Deliverables:**
- [ ] `bd orchestrate` command family
- [ ] `bd agent` command family
- [ ] `bd executions` command family
- [ ] RPC protocol extensions
- [ ] End-to-end tests

**Files to Create/Modify:**
```
cmd/bd/
├── orchestrate.go      # NEW: bd orchestrate commands
├── agent.go            # NEW: bd agent commands
├── executions.go       # NEW: bd executions commands
└── main.go             # MODIFY: register new commands

internal/rpc/
├── protocol.go         # MODIFY: add orchestration ops
└── server_orchestration.go  # NEW: orchestration RPC handlers
```

**User Stories:**
```bash
# As an admin, I can start orchestration
bd orchestrate start --interval=5s --timeout=30m

# As an agent, I can register myself
bd agent register --id=my-agent --capabilities=go,testing

# As an agent, I can send heartbeats
bd agent heartbeat my-agent

# As a user, I can see what's running
bd executions list --status=running

# As a user, I can retry failed work
bd executions retry exec-abc123
```

### Phase 3: Dispatcher (Week 4-5)

**Goal:** Connect engine to real agents

**Tasks:**
1. Implement `Dispatcher` interface
2. Create `MCPDispatcher` for MCP server integration
3. Create `RPCDispatcher` for direct RPC calls
4. Create `CallbackDispatcher` for webhook-style dispatch
5. Add dispatch protocol configuration
6. Handle agent responses and status updates
7. Integration tests with real MCP server

**Deliverables:**
- [ ] Dispatcher abstraction
- [ ] MCP integration
- [ ] RPC integration
- [ ] Webhook integration
- [ ] Agent response handling
- [ ] E2E tests with real agents

**Files to Create:**
```
internal/orchestration/
├── dispatcher.go           # Dispatcher interface
├── dispatcher_mcp.go       # MCP protocol
├── dispatcher_rpc.go       # RPC protocol
├── dispatcher_callback.go  # Webhook protocol
└── dispatcher_test.go
```

**Dispatch Protocol Interface:**
```go
type DispatchProtocol interface {
    // Send work to agent
    Send(agent *Agent, issue *types.Issue, exec *Execution) error

    // Name of the protocol
    Name() string
}

// MCP implementation
type MCPDispatcher struct {
    mcpServerURL string
}

func (d *MCPDispatcher) Send(agent *Agent, issue *types.Issue, exec *Execution) error {
    // Call MCP server endpoint
    payload := map[string]interface{}{
        "agent_id":     agent.ID,
        "issue_id":     issue.ID,
        "execution_id": exec.ID,
        "title":        issue.Title,
        "description":  issue.Description,
    }

    resp, err := http.Post(
        d.mcpServerURL+"/execute",
        "application/json",
        marshalJSON(payload),
    )

    if err != nil {
        return err
    }

    // Parse response and update execution
    // ...
}
```

### Phase 4: Observability & Polish (Week 5-6)

**Goal:** Production-ready features

**Tasks:**
1. Add execution history queries
2. Implement execution filtering/search
3. Add metrics (execution duration, success rate, etc.)
4. Create execution timeline visualization
5. Add `bd orchestrate stats` command
6. Improve error messages and help text
7. Write comprehensive documentation
8. Performance testing with 1000+ issues

**Deliverables:**
- [ ] Advanced execution queries
- [ ] Metrics and statistics
- [ ] Timeline visualization
- [ ] Comprehensive docs
- [ ] Performance benchmarks
- [ ] User guide with examples

**Files to Create:**
```
docs/
├── orchestration.md        # User guide
├── orchestration-api.md    # API reference
└── orchestration-examples.md  # Common patterns

internal/orchestration/
├── metrics.go              # Metrics collection
├── timeline.go             # Execution timeline
└── stats.go                # Statistics queries
```

**Metrics to Track:**
```go
type OrchestrationMetrics struct {
    TotalExecutions    int64
    SuccessfulExecs    int64
    FailedExecs        int64
    TimeoutExecs       int64
    AverageDuration    time.Duration
    MedianDuration     time.Duration
    P95Duration        time.Duration

    AgentUtilization   map[string]float64  // agent_id -> % busy
    CapabilityDemand   map[string]int      // capability -> queue depth
}
```

### Phase 5: Advanced Features (Future)

**Goal:** Power user capabilities

**Tasks (not in initial scope, for future consideration):**
1. Workflow DSL for complex multi-issue flows
2. Conditional execution (if/else logic)
3. Parallel execution groups
4. Manual approval gates
5. Execution templates
6. Agent auto-scaling hints
7. Cross-repo orchestration
8. Execution event webhooks

**Potential Features:**
```bash
# Workflow definition file
bd orchestrate workflow create --file=auth-workflow.yml

# Approval gate
bd executions approve exec-abc123

# Parallel group execution
bd orchestrate run --parallel --label=batch-2025-12
```

---

## Integration Points

### 1. Daemon Lifecycle

Orchestration engine runs within existing daemon process:

```go
// cmd/bd/daemon.go modifications
func runDaemonLoop(...) {
    // ... existing daemon setup ...

    // Initialize orchestration (if enabled)
    if orchestrationEnabled {
        engine, err := orchestration.NewEngine(store, config)
        if err != nil {
            log.log("Warning: orchestration disabled: %v", err)
        } else {
            go engine.Run()
            defer engine.Stop()
        }
    }

    // ... existing event loop ...
}
```

### 2. Ready Work Integration

Engine uses existing `GetReadyWork()`:

```go
func (e *Engine) getReadyWork() []*types.Issue {
    filter := types.WorkFilter{
        Status: "",  // Get both open and in_progress
        Limit:  100, // Batch size
    }

    issues, err := e.store.GetReadyWork(e.ctx, filter)
    if err != nil {
        e.log("Error fetching ready work: %v", err)
        return nil
    }

    // Filter out issues already in execution
    return e.filterAlreadyAssigned(issues)
}
```

### 3. MCP Server Integration

MCP server becomes an orchestration client:

```python
# integrations/beads-mcp/src/beads_mcp/agent.py

class BeadsAgent:
    def __init__(self, agent_id: str, capabilities: List[str]):
        self.agent_id = agent_id
        self.capabilities = capabilities

    async def register(self):
        """Register with orchestrator"""
        await self.rpc_call("agent_register", {
            "id": self.agent_id,
            "capabilities": self.capabilities
        })

    async def heartbeat(self):
        """Send heartbeat to stay alive"""
        await self.rpc_call("agent_heartbeat", {
            "id": self.agent_id
        })

    async def poll_for_work(self):
        """Poll for assigned work"""
        response = await self.rpc_call("execution_list", {
            "agent_id": self.agent_id,
            "status": "queued"
        })

        executions = response["data"]
        for exec in executions:
            await self.execute(exec)

    async def execute(self, execution: dict):
        """Execute assigned work"""
        issue_id = execution["issue_id"]
        exec_id = execution["id"]

        try:
            # Update to running
            await self.update_execution(exec_id, "running")

            # Do work...
            result = await self.do_work(issue_id)

            # Update to completed
            await self.update_execution(exec_id, "completed", result)

        except Exception as e:
            # Update to failed
            await self.update_execution(exec_id, "failed", str(e))
```

### 4. Event System Integration

Orchestration events flow into existing events table:

```go
// When execution state changes, log event
func (m *ExecutionManager) UpdateStatus(id string, status Status, err error) error {
    // ... update execution record ...

    // Log event for audit trail
    event := &types.Event{
        IssueID:   exec.IssueID,
        EventType: "orchestration.status_change",
        Actor:     exec.AgentID,
        Details: map[string]interface{}{
            "execution_id": id,
            "old_status":   exec.Status,
            "new_status":   status,
            "error":        err,
        },
    }

    return m.store.CreateEvent(ctx, event)
}
```

---

## Configuration

### Daemon Configuration

Add to `.beads/config.yaml` (or via `bd config set`):

```yaml
orchestration:
  enabled: true
  engine:
    tick_interval: "5s"
    default_timeout: "30m"
    max_retries: 3
    heartbeat_timeout: "60s"

  dispatcher:
    protocol: "mcp"  # mcp | rpc | callback
    mcp_url: "http://localhost:8080"

  agents:
    auto_offline_threshold: "120s"
```

### Environment Variables

```bash
# Enable orchestration
export BEADS_ORCHESTRATION_ENABLED=true

# Override tick interval
export BEADS_ORCHESTRATION_INTERVAL=10s

# Set dispatch protocol
export BEADS_DISPATCH_PROTOCOL=mcp
export BEADS_MCP_URL=http://localhost:8080
```

---

## Testing Strategy

### Unit Tests

**Coverage Requirements:**
- 80%+ code coverage for all orchestration code
- 100% coverage for critical paths (timeout, retry logic)

**Test Categories:**
1. Schema tests (table creation, constraints)
2. Execution manager tests (CRUD, state transitions)
3. Agent registry tests (capability matching, heartbeat)
4. Engine tests (dispatch logic, timeout handling)
5. Dispatcher tests (protocol implementations)

### Integration Tests

**Scenarios:**
1. Full orchestration loop with mock agents
2. Dependency resolution triggering dispatch
3. Timeout and retry handling
4. Agent failure and reassignment
5. Concurrent execution handling

### End-to-End Tests

**User Workflows:**
1. Register agent → create issue → automatic dispatch → completion
2. Multi-step workflow with dependencies
3. Agent goes offline → work reassigned
4. Failed execution → automatic retry → success
5. Timeout → retry → max retries → manual intervention

### Performance Tests

**Benchmarks:**
1. 1,000 issues, 10 agents → dispatch time
2. 10,000 execution history → query performance
3. 100 concurrent agents → registry lookup speed
4. Timeout checking with 1,000 running executions

**Acceptance Criteria:**
- Dispatch latency: < 100ms (p95)
- Execution query: < 50ms for 10k records
- Agent matching: < 10ms for 100 agents
- Memory usage: < 50MB for orchestration layer

---

## Migration Strategy

### Rollout Plan

**Phase A: Opt-in Beta (v0.31)**
- Feature flag: `orchestration.enabled=false` by default
- Early adopters enable manually
- Gather feedback, iterate on UX
- No breaking changes to existing workflows

**Phase B: Stable Release (v0.32)**
- Feature flag: `orchestration.enabled=false` (still opt-in)
- Production-ready documentation
- MCP server integration complete
- Performance validated at scale

**Phase C: General Availability (v0.33+)**
- Feature flag: `orchestration.enabled=true` by default
- Auto-migration creates tables on first daemon start
- Comprehensive tutorials and examples
- Consider for v1.0 milestone feature set

### Backward Compatibility

**Guarantees:**
1. Existing `bd` commands work unchanged
2. JSONL format unchanged (orchestration tables not exported)
3. Existing daemons work without orchestration
4. No performance impact when `orchestration.enabled=false`

**Database Compatibility:**
- Orchestration tables use `orchestration_` prefix
- Foreign keys cascade deletes (no orphaned records)
- Tables can be dropped without affecting core beads functionality

---

## Security Considerations

### Threat Model

**Threats:**
1. **Malicious Agent Registration**: Rogue agent claims false capabilities
2. **Execution Hijacking**: Attacker completes execution intended for different agent
3. **Resource Exhaustion**: Agent creates infinite loop of failed executions
4. **Data Exfiltration**: Agent accesses issues beyond its assignments

**Mitigations:**
1. **Agent Authentication**: Require API tokens for registration (Phase 2+)
2. **Execution Validation**: Verify agent ID matches assigned agent before accepting updates
3. **Rate Limiting**: Max executions per agent per minute
4. **Capability Isolation**: Agents only see issues matching their capabilities

### Access Control

**Current Implementation (Phase 1):**
- No authentication (trust boundary is daemon socket)
- Daemon socket uses filesystem permissions (Unix sockets)
- Agents must have access to workspace to register

**Future Enhancement (Phase 2+):**
```go
type Agent struct {
    // ... existing fields ...
    APIKey      string    // For authentication
    Permissions []string  // Allowed operations
}

func (r *AgentRegistry) Register(agent *Agent, apiKey string) error {
    // Verify API key
    if !r.validateAPIKey(apiKey) {
        return ErrUnauthorized
    }

    // Rate limit registrations
    if r.exceedsRateLimit(apiKey) {
        return ErrRateLimited
    }

    // ... existing logic ...
}
```

---

## Performance Considerations

### Scalability Targets

**Issue Scale:**
- 10,000 issues in database
- 1,000 issues in ready queue
- 100 concurrent executions
- 50 registered agents

**Performance SLOs:**
- Ready work query: < 100ms
- Agent matching: < 50ms
- Dispatch latency: < 100ms (p95)
- Heartbeat processing: < 10ms
- Timeout checking: < 500ms for 1,000 executions

### Optimization Strategies

**1. Blocked Issues Cache Reuse**
- Leverage existing `blocked_issues_cache` table
- No need to recalculate ready work
- Engine only queries cache invalidation events

**2. Agent Capability Indexing**
```sql
-- Denormalize capabilities for fast lookup
CREATE INDEX idx_agent_capabilities ON orchestration_agents(
    json_extract(capabilities, '$[0]'),
    json_extract(capabilities, '$[1]'),
    json_extract(capabilities, '$[2]')
) WHERE status = 'idle';
```

**3. Execution Pagination**
```go
// Don't load all executions into memory
func (m *ExecutionManager) StreamExecutions(filter ExecutionFilter) <-chan *Execution {
    ch := make(chan *Execution, 100)

    go func() {
        offset := 0
        limit := 100

        for {
            execs, err := m.listExecutionsPaginated(filter, offset, limit)
            if err != nil || len(execs) == 0 {
                close(ch)
                return
            }

            for _, exec := range execs {
                ch <- exec
            }

            offset += limit
        }
    }()

    return ch
}
```

**4. Batch Operations**
```go
// Update multiple executions in single transaction
func (m *ExecutionManager) BatchUpdateStatus(updates []StatusUpdate) error {
    return m.db.RunInTransaction(func(tx *sql.Tx) error {
        stmt, err := tx.Prepare(`
            UPDATE orchestration_executions
            SET status = ?, updated_at = ?, last_error = ?
            WHERE id = ?
        `)
        defer stmt.Close()

        for _, u := range updates {
            _, err = stmt.Exec(u.Status, time.Now(), u.Error, u.ID)
            if err != nil {
                return err
            }
        }

        return nil
    })
}
```

---

## Documentation Deliverables

### User Documentation

**1. Orchestration Guide** (`docs/orchestration.md`)
- Conceptual overview
- Getting started tutorial
- Common workflows
- Troubleshooting guide

**2. API Reference** (`docs/orchestration-api.md`)
- RPC protocol specification
- CLI command reference
- Agent implementation guide
- Database schema reference

**3. Examples** (`docs/orchestration-examples.md`)
- Simple agent implementation (Python)
- Multi-agent workflow example
- Integration with CI/CD
- Monitoring and alerting patterns

### Developer Documentation

**1. Architecture Doc** (`internal/orchestration/README.md`)
- Component overview
- Design decisions
- Extension points
- Testing strategy

**2. Migration Guide** (for contributors)
- How to add new execution statuses
- How to implement new dispatch protocols
- How to extend capability matching

---

## Success Metrics

### Adoption Metrics

**Phase 1 (Opt-in Beta):**
- 5+ teams using orchestration in production
- 10+ agents registered across all users
- 100+ automated executions per week

**Phase 2 (General Availability):**
- 20+ teams using orchestration
- 50+ agents registered
- 1,000+ automated executions per week

### Quality Metrics

**Reliability:**
- 99%+ execution success rate (excluding user code failures)
- < 1% orphaned executions (no timeout or completion)
- 0 data corruption incidents

**Performance:**
- < 100ms dispatch latency (p95)
- < 50ms ready work query (p95)
- < 10MB memory overhead per daemon

### User Satisfaction

**NPS Goals:**
- NPS > 50 for orchestration users
- < 5% feature removal requests
- > 80% "would recommend" rating

---

## Risk Assessment

### Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Schema migration issues | Medium | High | Extensive testing, rollback plan |
| Performance degradation | Low | Medium | Benchmarking, feature flag |
| Daemon stability issues | Low | High | Crash recovery, health checks |
| Race conditions | Medium | Medium | Transaction isolation, locks |

### Product Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Feature complexity too high | Medium | High | Iterative rollout, user testing |
| Low adoption | Low | Medium | Clear documentation, examples |
| Conflicts with existing workflows | Low | High | Backward compatibility guarantee |

### Operational Risks

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Support burden increases | Medium | Medium | Comprehensive docs, self-service tools |
| Breaking changes required | Low | High | Semantic versioning, deprecation policy |
| Maintenance overhead | Medium | Medium | Automated testing, monitoring |

---

## Alternatives Considered

### Alternative 1: External Orchestrator (e.g., Temporal, Airflow)

**Pros:**
- Mature, battle-tested
- Rich feature set
- Active community

**Cons:**
- Requires separate infrastructure
- Complex setup/configuration
- Breaks beads' "local-first" philosophy
- Additional dependencies

**Decision:** Rejected - violates core design principles

### Alternative 2: GitHub Actions Integration

**Pros:**
- Familiar to developers
- Native CI/CD integration
- Free for open source

**Cons:**
- Requires GitHub (lock-in)
- Not local-first
- Limited to GitHub ecosystem
- Slow execution times

**Decision:** Rejected - too narrow, not self-hosted

### Alternative 3: Lightweight Callback System

**Pros:**
- Simpler implementation
- Less code to maintain
- Easier to understand

**Cons:**
- No agent registry
- No automatic retries
- No execution history
- Manual load balancing

**Decision:** Rejected - insufficient value add

### Selected Approach: Embedded Orchestration

**Why:**
- Aligns with beads architecture (SQLite, daemon, local-first)
- Uses existing extension pattern (custom tables)
- No external dependencies
- Scales with beads infrastructure
- Can evolve incrementally

---

## Open Questions

### To Resolve Before Implementation

1. **Agent Communication Protocol**
   - Should we support multiple protocols from day 1?
   - What's the minimum viable dispatch interface?
   - How do agents report progress (streaming vs polling)?

2. **Execution Timeouts**
   - Should timeout be configurable per-issue (via label)?
   - What's reasonable default timeout for different issue types?
   - Should we support "infinite" timeout for long-running tasks?

3. **Capability Model**
   - How granular should capabilities be? (e.g., "go" vs "go.testing" vs "go.testing.integration")
   - Should capabilities be hierarchical?
   - How to handle capability versioning (e.g., "python3.9" vs "python3.11")?

4. **Multi-Repo Orchestration**
   - Should orchestration work across repos (bd-4ms)?
   - How to handle cross-repo dependencies in dispatch?
   - Should agents be repo-scoped or global?

5. **Execution Cleanup**
   - How long to retain execution history?
   - Should we compact old executions like issues?
   - What's the retention policy (30 days? 90 days? forever?)?

### To Resolve During Implementation

1. **Error Handling**
   - What error codes should agents return?
   - How to distinguish retryable vs permanent failures?
   - Should we support error classification?

2. **Observability**
   - What metrics are most useful?
   - Should we integrate with Prometheus/Grafana?
   - How to surface execution logs to users?

3. **Agent Discovery**
   - Should agents auto-register via service discovery?
   - Support for agent pools (multiple instances of same capability)?
   - How to handle agent version skew?

---

## Next Steps

### Immediate Actions (Pre-Implementation)

1. **User Feedback Round**
   - [ ] Share plan with 3-5 power users
   - [ ] Gather feedback on CLI UX
   - [ ] Validate capability model makes sense
   - [ ] Confirm dispatch protocol meets needs

2. **Technical Validation**
   - [ ] Prototype schema in test database
   - [ ] Validate foreign key constraints work as expected
   - [ ] Benchmark ready work query with orchestration tables
   - [ ] Confirm daemon can handle orchestration loop

3. **Documentation Prep**
   - [ ] Create `docs/orchestration/` directory
   - [ ] Draft getting-started tutorial
   - [ ] Write agent implementation guide
   - [ ] Prepare example agent code

### Implementation Kickoff

1. **Create Epic and Tasks**
   ```bash
   bd create --type=epic --title="Beads Orchestra: Workflow Orchestration" --id=bd-orchestra
   bd create --type=task --title="Phase 0: Foundation - Schema and Package Setup" --parent=bd-orchestra
   bd create --type=task --title="Phase 1: Core Engine Implementation" --parent=bd-orchestra
   bd create --type=task --title="Phase 2: CLI Integration" --parent=bd-orchestra
   bd create --type=task --title="Phase 3: Dispatcher Implementation" --parent=bd-orchestra
   bd create --type=task --title="Phase 4: Observability and Polish" --parent=bd-orchestra
   ```

2. **Set Up Development Environment**
   - [ ] Create feature branch `feature/orchestration`
   - [ ] Set up test database with sample data
   - [ ] Configure CI to run orchestration tests
   - [ ] Add orchestration to code coverage reports

3. **Begin Phase 0**
   - [ ] Implement schema definitions
   - [ ] Write schema tests
   - [ ] Create package structure
   - [ ] Submit PR for review

---

## Appendix A: Complete SQL Schema

```sql
-- ============================================================================
-- Beads Orchestra Database Schema
-- Version: 1.0
-- Description: Workflow orchestration tables for AI agent coordination
-- ============================================================================

-- ----------------------------------------------------------------------------
-- Table: orchestration_executions
-- Description: Tracks individual work item executions
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS orchestration_executions (
    -- Identity
    id TEXT PRIMARY KEY NOT NULL,           -- Format: exec-{6-char-hash}
    issue_id TEXT NOT NULL,                 -- FK to issues.id
    workflow_id TEXT,                       -- Optional: parent workflow (future)

    -- Agent assignment
    agent_id TEXT,                          -- FK to orchestration_agents.id (NULL if queued)
    assigned_at INTEGER,                    -- Unix timestamp milliseconds

    -- Execution lifecycle
    status TEXT NOT NULL CHECK(status IN (
        'queued', 'running', 'completed', 'failed', 'cancelled', 'timeout'
    )),
    started_at INTEGER,                     -- Unix timestamp milliseconds
    completed_at INTEGER,                   -- Unix timestamp milliseconds
    timeout_at INTEGER,                     -- Unix timestamp milliseconds (deadline)

    -- Retry logic
    retry_count INTEGER NOT NULL DEFAULT 0,
    max_retries INTEGER NOT NULL DEFAULT 3,
    last_error TEXT,                        -- Error message from last failure

    -- Metadata
    created_at INTEGER NOT NULL,            -- Unix timestamp milliseconds
    updated_at INTEGER NOT NULL,            -- Unix timestamp milliseconds
    created_by TEXT,                        -- User or system that created execution

    -- Result data
    result_data TEXT,                       -- JSON blob for execution results

    -- Foreign key constraints
    FOREIGN KEY (issue_id) REFERENCES issues(id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES orchestration_agents(id) ON DELETE SET NULL
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_orchestration_executions_issue
    ON orchestration_executions(issue_id);

CREATE INDEX IF NOT EXISTS idx_orchestration_executions_agent
    ON orchestration_executions(agent_id)
    WHERE agent_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_orchestration_executions_status
    ON orchestration_executions(status);

CREATE INDEX IF NOT EXISTS idx_orchestration_executions_timeout
    ON orchestration_executions(timeout_at)
    WHERE status = 'running';

CREATE INDEX IF NOT EXISTS idx_orchestration_executions_retry
    ON orchestration_executions(retry_count, max_retries, status)
    WHERE status = 'failed' AND retry_count < max_retries;

-- ----------------------------------------------------------------------------
-- Table: orchestration_agents
-- Description: Registry of available agents and their capabilities
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS orchestration_agents (
    -- Identity
    id TEXT PRIMARY KEY NOT NULL,           -- Agent identifier (user-provided)

    -- Capabilities (stored as JSON array of strings)
    capabilities TEXT NOT NULL,             -- JSON: ["go", "python", "testing"]

    -- Agent status
    status TEXT NOT NULL CHECK(status IN (
        'idle', 'busy', 'offline', 'paused'
    )),
    current_task TEXT,                      -- FK to orchestration_executions.id

    -- Lifecycle timestamps
    last_heartbeat INTEGER NOT NULL,        -- Unix timestamp milliseconds
    registered_at INTEGER NOT NULL,         -- Unix timestamp milliseconds
    last_seen INTEGER,                      -- Unix timestamp milliseconds

    -- Configuration
    max_concurrent_tasks INTEGER NOT NULL DEFAULT 1,
    priority INTEGER NOT NULL DEFAULT 0,    -- Higher = preferred

    -- Metadata (JSON blob for agent-specific config)
    metadata TEXT,                          -- JSON: {"version": "1.0", ...}

    -- Foreign key constraints
    FOREIGN KEY (current_task) REFERENCES orchestration_executions(id) ON DELETE SET NULL
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_orchestration_agents_status
    ON orchestration_agents(status);

CREATE INDEX IF NOT EXISTS idx_orchestration_agents_heartbeat
    ON orchestration_agents(last_heartbeat);

CREATE INDEX IF NOT EXISTS idx_orchestration_agents_priority
    ON orchestration_agents(priority DESC, status);

-- ----------------------------------------------------------------------------
-- Table: orchestration_workflows (Future - Phase 5+)
-- Description: Complex multi-issue workflows with conditional logic
-- ----------------------------------------------------------------------------
-- CREATE TABLE IF NOT EXISTS orchestration_workflows (
--     id TEXT PRIMARY KEY NOT NULL,
--     name TEXT NOT NULL,
--     definition TEXT NOT NULL,            -- JSON workflow DSL
--     status TEXT NOT NULL,
--     created_at INTEGER NOT NULL,
--     updated_at INTEGER NOT NULL
-- );

-- ============================================================================
-- Views (for convenience)
-- ============================================================================

-- View: Active executions with issue details
CREATE VIEW IF NOT EXISTS v_orchestration_active_executions AS
SELECT
    e.id AS execution_id,
    e.issue_id,
    i.title AS issue_title,
    i.priority AS issue_priority,
    e.agent_id,
    a.status AS agent_status,
    e.status AS execution_status,
    e.started_at,
    e.timeout_at,
    (e.timeout_at - CAST(strftime('%s', 'now') || substr(strftime('%f', 'now'), 4) AS INTEGER)) AS time_remaining_ms,
    e.retry_count,
    e.max_retries
FROM orchestration_executions e
JOIN issues i ON e.issue_id = i.id
LEFT JOIN orchestration_agents a ON e.agent_id = a.id
WHERE e.status IN ('queued', 'running');

-- View: Agent utilization
CREATE VIEW IF NOT EXISTS v_orchestration_agent_utilization AS
SELECT
    a.id AS agent_id,
    a.status,
    a.capabilities,
    a.priority,
    COUNT(e.id) AS total_executions,
    SUM(CASE WHEN e.status = 'completed' THEN 1 ELSE 0 END) AS completed_executions,
    SUM(CASE WHEN e.status = 'failed' THEN 1 ELSE 0 END) AS failed_executions,
    AVG(CASE
        WHEN e.completed_at IS NOT NULL AND e.started_at IS NOT NULL
        THEN e.completed_at - e.started_at
        ELSE NULL
    END) AS avg_execution_time_ms,
    (CAST(strftime('%s', 'now') || substr(strftime('%f', 'now'), 4) AS INTEGER) - a.last_heartbeat) AS last_heartbeat_age_ms
FROM orchestration_agents a
LEFT JOIN orchestration_executions e ON a.id = e.agent_id
GROUP BY a.id;

-- ============================================================================
-- End of Schema
-- ============================================================================
```

---

## Appendix B: Example Agent Implementation

### Python Agent Example

```python
#!/usr/bin/env python3
"""
Beads Orchestra Agent - Example Implementation
"""
import asyncio
import json
import subprocess
import sys
from typing import List, Dict, Any
from datetime import datetime

class BeadsAgent:
    def __init__(self, agent_id: str, capabilities: List[str]):
        self.agent_id = agent_id
        self.capabilities = capabilities
        self.running = True

    async def register(self):
        """Register agent with orchestrator"""
        result = self._run_bd_command([
            "agent", "register",
            "--id", self.agent_id,
            "--capabilities", ",".join(self.capabilities),
            "--json"
        ])

        print(f"[{self._timestamp()}] Registered agent: {self.agent_id}")
        print(f"  Capabilities: {', '.join(self.capabilities)}")

    async def heartbeat_loop(self):
        """Send periodic heartbeats"""
        while self.running:
            try:
                self._run_bd_command(["agent", "heartbeat", self.agent_id])
                await asyncio.sleep(30)  # Heartbeat every 30s
            except Exception as e:
                print(f"[{self._timestamp()}] Heartbeat failed: {e}")
                await asyncio.sleep(5)

    async def work_loop(self):
        """Poll for assigned work and execute"""
        while self.running:
            try:
                # Poll for queued executions
                result = self._run_bd_command([
                    "executions", "list",
                    "--agent", self.agent_id,
                    "--status", "queued",
                    "--json"
                ])

                executions = json.loads(result)

                for exec in executions:
                    await self.execute(exec)

                await asyncio.sleep(5)  # Poll every 5s

            except Exception as e:
                print(f"[{self._timestamp()}] Work loop error: {e}")
                await asyncio.sleep(10)

    async def execute(self, execution: Dict[str, Any]):
        """Execute assigned work"""
        exec_id = execution["id"]
        issue_id = execution["issue_id"]

        print(f"\n[{self._timestamp()}] Starting execution: {exec_id}")
        print(f"  Issue: {issue_id}")

        try:
            # Update status to running
            self._update_execution(exec_id, "running")

            # Fetch issue details
            issue = self._get_issue(issue_id)
            print(f"  Title: {issue['title']}")

            # Do the actual work
            result = await self.do_work(issue)

            # Update status to completed
            self._update_execution(exec_id, "completed", result)

            print(f"[{self._timestamp()}] Completed execution: {exec_id}")

        except Exception as e:
            error_msg = str(e)
            print(f"[{self._timestamp()}] Failed execution {exec_id}: {error_msg}")

            # Update status to failed
            self._update_execution(exec_id, "failed", error=error_msg)

    async def do_work(self, issue: Dict[str, Any]) -> Dict[str, Any]:
        """
        Override this method to implement actual work logic.
        This example just sleeps to simulate work.
        """
        print(f"  Simulating work...")
        await asyncio.sleep(5)

        return {
            "status": "success",
            "message": "Work completed successfully",
            "timestamp": datetime.now().isoformat()
        }

    def _run_bd_command(self, args: List[str]) -> str:
        """Run bd command and return stdout"""
        cmd = ["bd"] + args
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            check=True
        )
        return result.stdout.strip()

    def _get_issue(self, issue_id: str) -> Dict[str, Any]:
        """Fetch issue details"""
        result = self._run_bd_command(["show", issue_id, "--json"])
        return json.loads(result)

    def _update_execution(self, exec_id: str, status: str,
                         result: Dict = None, error: str = None):
        """Update execution status"""
        args = ["executions", "update", exec_id, "--status", status]

        if result:
            args.extend(["--result", json.dumps(result)])

        if error:
            args.extend(["--error", error])

        self._run_bd_command(args)

    def _timestamp(self) -> str:
        """Get current timestamp for logging"""
        return datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    async def run(self):
        """Main agent loop"""
        await self.register()

        # Start background tasks
        tasks = [
            asyncio.create_task(self.heartbeat_loop()),
            asyncio.create_task(self.work_loop())
        ]

        try:
            await asyncio.gather(*tasks)
        except KeyboardInterrupt:
            print(f"\n[{self._timestamp()}] Shutting down agent...")
            self.running = False

            # Wait for tasks to complete
            for task in tasks:
                task.cancel()
                try:
                    await task
                except asyncio.CancelledError:
                    pass

            print(f"[{self._timestamp()}] Agent stopped")


# Example usage
if __name__ == "__main__":
    agent = BeadsAgent(
        agent_id="example-agent",
        capabilities=["python", "testing", "research"]
    )

    asyncio.run(agent.run())
```

### Running the Example

```bash
# Terminal 1: Start orchestration
bd orchestrate start

# Terminal 2: Run agent
python3 agent.py

# Terminal 3: Create test work
bd create --title="Test orchestration task" --labels=requires:python
```

---

## Appendix C: Glossary

**Agent**: A worker process that executes assigned tasks. Can be a human (via CLI), AI agent (via MCP), or automated service (via API).

**Capability**: A skill or technology that an agent can handle (e.g., "go", "python", "database", "testing").

**Dispatcher**: Component responsible for sending work assignments to agents via a protocol (MCP, RPC, webhook).

**Execution**: A single attempt to complete a specific issue. Includes lifecycle tracking (queued → running → completed/failed).

**Orchestration Engine**: The core loop that finds ready work, matches to agents, dispatches, and monitors execution.

**Ready Work**: Issues with no open blockers (dependencies resolved) and status of open/in_progress.

**Retry**: Automatic re-execution of a failed task, up to max_retries limit.

**Timeout**: Maximum duration for an execution. After timeout, execution is marked as failed and retried (if retries available).

**Workflow**: (Future) A coordinated sequence of issues with conditional logic and parallel execution support.

---

## Appendix D: References

**Existing Beads Documentation:**
- `EXTENDING.md` - Custom tables pattern
- `AGENTS.md` - Agent architecture overview
- `ARCHITECTURE.md` - System design
- `docs/daemon.md` - Daemon lifecycle

**Similar Systems:**
- [Temporal.io](https://temporal.io) - Durable execution platform
- [Airflow](https://airflow.apache.org) - Workflow orchestration
- [Prefect](https://www.prefect.io) - Data workflow orchestration
- [Celery](https://docs.celeryproject.org) - Distributed task queue

**Relevant Issues:**
- bd-4ms: Multi-repo support (orchestration should handle)
- bd-165: Ready work includes in_progress (useful for orchestration)
- bd-5qim: Blocked cache optimization (orchestration leverages)

---

**END OF IMPLEMENTATION PLAN**

---

*This plan is a living document. As implementation progresses, update with:*
- *Decisions made on open questions*
- *Lessons learned during development*
- *User feedback and feature requests*
- *Performance benchmarks and optimization results*
