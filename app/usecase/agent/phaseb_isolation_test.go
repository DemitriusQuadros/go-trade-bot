package agent_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	agentusecase "go-trade-bot/app/usecase/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B-01 AC#9 / safety invariant: no LLM-reachable code path can call
// StrategyRepository.ReplaceScriptSource (the only write path that changes
// a live strategy's code). Three independent checks:
//  1. app/usecase/agent (the tool registry, shared by chat, cmd/agent runs
//     and MCP) does not transitively import the strategy repository or the
//     proposal usecase/applier - so it cannot reach ReplaceScriptSource;
//  2. no non-test source file under app/usecase/agent references a
//     ReplaceScriptSource identifier;
//  3. no dependency interface on AgentUseCase exposes a ReplaceScriptSource
//     method.

const modulePath = "go-trade-bot"

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir, "go.mod not found")
		dir = parent
	}
}

// transitiveModuleImports walks non-test imports of pkg within this module.
func transitiveModuleImports(t *testing.T, root, pkg string) map[string]bool {
	t.Helper()
	seen := map[string]bool{}
	var walk func(p string)
	walk = func(p string) {
		if seen[p] {
			return
		}
		seen[p] = true
		dir := filepath.Join(root, strings.TrimPrefix(p, modulePath+"/"))
		bp, err := build.Default.ImportDir(dir, 0)
		if err != nil {
			if _, ok := err.(*build.NoGoError); ok {
				return
			}
			require.NoError(t, err, p)
		}
		for _, imp := range bp.Imports {
			if strings.HasPrefix(imp, modulePath+"/") {
				walk(imp)
			}
		}
	}
	walk(pkg)
	return seen
}

func TestPhaseB_AgentToolsCannotReachReplaceScriptSource(t *testing.T) {
	root := moduleRoot(t)
	deps := transitiveModuleImports(t, root, modulePath+"/app/usecase/agent")
	require.True(t, deps[modulePath+"/app/usecase/backtest"], "sanity: the walk follows imports")
	for _, forbidden := range []string{
		modulePath + "/app/repository/strategy",
		modulePath + "/app/usecase/proposal",
		modulePath + "/app/handler/tasks/agent",
	} {
		assert.Falsef(t, deps[forbidden], "app/usecase/agent must not (transitively) import %s", forbidden)
	}

	// No identifier named ReplaceScriptSource in the agent usecase's code.
	fset := token.NewFileSet()
	agentDir := filepath.Join(root, "app/usecase/agent")
	require.NoError(t, filepath.Walk(agentDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "ReplaceScriptSource" {
				t.Errorf("%s references ReplaceScriptSource", fset.Position(id.Pos()))
			}
			return true
		})
		return nil
	}))

	// No dependency interface of AgentUseCase has the method.
	ucType := reflect.TypeOf(agentusecase.AgentUseCase{})
	for i := 0; i < ucType.NumField(); i++ {
		ft := ucType.Field(i).Type
		if ft.Kind() != reflect.Interface {
			continue
		}
		_, has := ft.MethodByName("ReplaceScriptSource")
		assert.Falsef(t, has, "AgentUseCase.%s exposes ReplaceScriptSource", ucType.Field(i).Name)
	}
}

// The Phase B tools are registered (over MCP for the default agent too) and
// no tool name suggests approving/applying a proposal or setting a
// strategy's status/mode - those capabilities are absent, not just refused.
func TestPhaseB_StrategyWritingToolSet(t *testing.T) {
	e := newPhaseBEnv(t)
	names := map[string]bool{}
	for _, tool := range e.uc.Tools() {
		names[tool.Def.Name] = true
	}
	for _, n := range []string{"deploy_to_testing", "create_challenger", "list_proposals", "get_deploy_gate_config", "save_strategy_script"} {
		assert.Truef(t, names[n], "%s should be registered for the default agent over MCP", n)
	}
	for n := range names {
		for _, bad := range []string{"approve", "apply", "replace", "promote_now", "set_status", "set_mode"} {
			assert.NotContainsf(t, n, bad, "no tool may approve/apply proposals or set status/mode (%s)", n)
		}
	}
}
