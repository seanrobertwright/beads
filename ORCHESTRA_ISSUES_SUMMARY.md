# Beads Orchestra: Issues Created

**Date:** 2025-12-19
**Epic ID:** `bd-1b126e`
**Total Tasks:** 23

---

## Epic

**ID:** `bd-1b126e`
**Title:** Beads Orchestra: Workflow Orchestration for AI Agent Coordination
**Type:** Epic
**Status:** Open
**Priority:** P2

**Description:**
Transform beads from a passive issue tracker into an active AI agent coordinator with automatic work dispatch, retry/timeout handling, intelligent load balancing, and full execution observability.

**Full Plan:** See [ORCHESTRATION_PLAN.md](./ORCHESTRATION_PLAN.md)

---

## Task Breakdown

### Phase 0: Foundation (5 tasks)

| ID | Title | Priority | Labels |
|----|-------|----------|--------|
| bd-1b126e.1 | Create internal/orchestration package structure | P1 | phase:0, requires:go |
| bd-1b126e.2 | Define orchestration database schema | P1 | phase:0, requires:database, requires:go |
| bd-1b126e.3 | Implement database initialization logic | P1 | phase:0, requires:go, requires:database |
| bd-1b126e.4 | Write comprehensive schema tests | P2 | phase:0, requires:go, requires:testing |
| bd-1b126e.5 | Create orchestration documentation | P2 | phase:0, requires:documentation |

**Goal:** Set up infrastructure without breaking changes

### Phase 1: Core Engine (4 tasks)

| ID | Title | Priority | Labels |
|----|-------|----------|--------|
| bd-1b126e.6 | Implement ExecutionManager | P1 | phase:1, requires:go |
| bd-1b126e.7 | Implement AgentRegistry | P1 | phase:1, requires:go |
| bd-1b126e.8 | Implement orchestration Engine | P1 | phase:1, requires:go |
| bd-1b126e.9 | Add orchestration integration tests | P2 | phase:1, requires:go, requires:testing |

**Goal:** Implement basic orchestration loop

### Phase 2: CLI Integration (5 tasks)

| ID | Title | Priority | Labels |
|----|-------|----------|--------|
| bd-1b126e.10 | Create bd orchestrate command | P1 | phase:2, requires:go, requires:cli |
| bd-1b126e.11 | Create bd agent command | P1 | phase:2, requires:go, requires:cli |
| bd-1b126e.12 | Create bd executions command | P1 | phase:2, requires:go, requires:cli |
| bd-1b126e.13 | Add orchestration RPC protocol | P1 | phase:2, requires:go, requires:rpc |
| bd-1b126e.14 | Integrate orchestration with daemon | P1 | phase:2, requires:go |

**Goal:** Add user-facing commands

### Phase 3: Dispatcher (4 tasks)

| ID | Title | Priority | Labels |
|----|-------|----------|--------|
| bd-1b126e.15 | Create Dispatcher interface | P1 | phase:3, requires:go |
| bd-1b126e.16 | Implement MCP dispatcher | P1 | phase:3, requires:go, requires:http |
| bd-1b126e.17 | Implement RPC dispatcher | P2 | phase:3, requires:go, requires:rpc |
| bd-1b126e.18 | Add agent response handling | P1 | phase:3, requires:go |

**Goal:** Connect engine to real agents

### Phase 4: Observability & Polish (5 tasks)

| ID | Title | Priority | Labels |
|----|-------|----------|--------|
| bd-1b126e.19 | Add execution history queries | P2 | phase:4, requires:go, requires:database |
| bd-1b126e.20 | Add orchestration metrics | P2 | phase:4, requires:go |
| bd-1b126e.21 | Create execution timeline visualization | P2 | phase:4, requires:go, requires:cli |
| bd-1b126e.22 | Write orchestration user guide | P2 | phase:4, requires:documentation |
| bd-1b126e.23 | Performance testing and optimization | P1 | phase:4, requires:go, requires:performance |

**Goal:** Production-ready features

---

## Quick Commands

```bash
# View the epic
bd show bd-1b126e

# List all tasks
bd list --parent=bd-1b126e

# List Phase 0 tasks (ready to start)
bd list --parent=bd-1b126e --label=phase:0

# Show first task
bd show bd-1b126e.1

# Start working on first task
bd update bd-1b126e.1 --status=in_progress

# See what's ready to work on
bd ready --label=phase:0
```

---

## Dependencies

All tasks have a `parent-child` dependency on the epic `bd-1b126e`, which means:
- They appear when you run `bd list --parent=bd-1b126e`
- They can be worked on independently within each phase
- Closing the epic requires all child tasks to be closed

**Suggested workflow:**
1. Work through Phase 0 sequentially (foundation)
2. Phase 1 tasks can be worked in parallel (with integration test last)
3. Phase 2 tasks can be worked in parallel
4. Phase 3 tasks should follow: interface → dispatchers → response handling
5. Phase 4 tasks can be worked in parallel

---

## Files Created

1. **ORCHESTRATION_PLAN.md** - 70-page detailed implementation plan
2. **create_orchestra_issues.js** - Script to generate issues
3. **.beads/orchestra_issues.jsonl** - Generated issues in JSONL format
4. **.beads/issues.jsonl** - Updated with all 24 new issues (1 epic + 23 tasks)

---

## Next Steps

1. **Review the plan**
   - Read ORCHESTRATION_PLAN.md thoroughly
   - Understand the architecture and design decisions
   - Ask questions or raise concerns

2. **Get feedback**
   - Share plan with 3-5 power users
   - Validate UX and CLI command design
   - Confirm capability model makes sense

3. **Start Phase 0**
   - Begin with bd-1b126e.1 (package structure)
   - Create feature branch: `git checkout -b feature/orchestration`
   - Follow TDD approach

4. **Track progress**
   - Update issue status as you work
   - Use `bd update` to track progress
   - Close tasks as you complete them

---

## Success Metrics

Track these metrics throughout implementation:

**Phase 0:**
- [ ] All schema tests pass with 100% coverage
- [ ] Package compiles without errors
- [ ] Documentation complete

**Phase 1:**
- [ ] Integration tests pass
- [ ] Engine runs without leaking goroutines
- [ ] Timeout detection <500ms latency

**Phase 2:**
- [ ] All CLI commands work with --json flag
- [ ] RPC protocol documented
- [ ] Help text comprehensive

**Phase 3:**
- [ ] MCP dispatcher works with beads-mcp
- [ ] Agent responses processed correctly
- [ ] Error handling robust

**Phase 4:**
- [ ] Dispatch latency <100ms (p95)
- [ ] Query performance <50ms for 10k records
- [ ] User guide complete with examples

---

**Status:** ✅ Epic and all tasks created and added to beads database

You can now start working on Phase 0!
