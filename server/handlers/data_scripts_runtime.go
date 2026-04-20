package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type scriptRuntimeResult struct {
	Operations []scriptRuntimeOperation `json:"operations"`
}

type scriptRuntimeOperation struct {
	Type      string         `json:"type"`
	OpType    string         `json:"op_type"`
	Table     string         `json:"table"`
	TableName string         `json:"table_name"`
	Key       map[string]any `json:"key"`
	PK        map[string]any `json:"pk"`
	Where     map[string]any `json:"where"`
	Values    map[string]any `json:"values"`
	After     map[string]any `json:"after"`
	Data      map[string]any `json:"data"`
}

type scriptRuntimeContext struct {
	ConnectionID int64  `json:"connection_id"`
	Database     string `json:"database"`
}

func buildDataScriptOperations(version *DataScriptVersion, connID int64, database string) ([]rawScriptOperation, error) {
	lang := normalizeDataScriptLanguage(version.Language)
	switch lang {
	case "javascript", "python", "php":
		return runNativeDataScript(version.SourceCode, lang, scriptRuntimeContext{
			ConnectionID: connID,
			Database:     database,
		})
	default:
		return nil, fmt.Errorf("unsupported data script language: %s", version.Language)
	}
}

func normalizeDataScriptLanguage(language string) string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "", "js", "javascript", "node":
		return "javascript"
	case "py", "python":
		return "python"
	case "php":
		return "php"
	default:
		return strings.ToLower(strings.TrimSpace(language))
	}
}

func runNativeDataScript(source, language string, runtimeCtx scriptRuntimeContext) ([]rawScriptOperation, error) {
	timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	payload, _ := json.Marshal(runtimeCtx)
	scriptBody, ext, err := buildNativeScriptFile(source, language)
	if err != nil {
		return nil, err
	}

	tmpDir, err := os.MkdirTemp("", "nias-data-script-*")
	if err != nil {
		return nil, fmt.Errorf("failed to prepare runtime workspace")
	}
	defer os.RemoveAll(tmpDir)

	scriptPath := filepath.Join(tmpDir, "script"+ext)
	if err := os.WriteFile(scriptPath, []byte(scriptBody), 0600); err != nil {
		return nil, fmt.Errorf("failed to write runtime script")
	}

	cmdName, args := runtimeCommand(language, scriptPath)
	cmd := exec.CommandContext(timeoutCtx, cmdName, args...)
	cmd.Env = append(os.Environ(), "NIAS_SCRIPT_CONTEXT="+string(payload))

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("script runtime failed: %s", trimRuntimeError(msg))
	}

	var result scriptRuntimeResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("script runtime returned invalid JSON plan")
	}
	if len(result.Operations) == 0 {
		return nil, fmt.Errorf("no plan operations returned by script")
	}
	return normalizeRuntimeOperations(result.Operations)
}

func runtimeCommand(language, scriptPath string) (string, []string) {
	switch language {
	case "python":
		return firstNonEmpty(os.Getenv("DATA_SCRIPT_PYTHON_BIN"), "python3"), []string{scriptPath}
	case "php":
		return firstNonEmpty(os.Getenv("DATA_SCRIPT_PHP_BIN"), "php"), []string{scriptPath}
	default:
		return firstNonEmpty(os.Getenv("DATA_SCRIPT_NODE_BIN"), "node"), []string{scriptPath}
	}
}

func buildNativeScriptFile(source, language string) (string, string, error) {
	switch language {
	case "javascript":
		return buildJavaScriptRuntime(source), ".js", nil
	case "python":
		return buildPythonRuntime(source), ".py", nil
	case "php":
		return buildPHPRuntime(source), ".php", nil
	default:
		return "", "", fmt.Errorf("unsupported language")
	}
}

func buildJavaScriptRuntime(source string) string {
	return `const context = JSON.parse(process.env.NIAS_SCRIPT_CONTEXT || "{}");
const operations = [];
const plan = {
  insert(table, values) { operations.push({ type: "insert", table, values: values || {} }); },
  update(table, key, values) { operations.push({ type: "update", table, key: key || {}, values: values || {} }); },
  delete(table, key) { operations.push({ type: "delete", table, key: key || {} }); },
};

(async () => {
  try {
` + indentLines(source, 4) + `
    process.stdout.write(JSON.stringify({ operations }));
  } catch (error) {
    const message = error && error.stack ? error.stack : String(error);
    console.error(message);
    process.exit(1);
  }
})();`
}

func buildPythonRuntime(source string) string {
	quotedSource, _ := json.Marshal(source)
	return `import json
import os
import sys
import traceback

context = json.loads(os.environ.get("NIAS_SCRIPT_CONTEXT", "{}"))
operations = []

class Plan:
    def insert(self, table, values):
        operations.append({"type": "insert", "table": table, "values": values or {}})

    def update(self, table, key, values):
        operations.append({"type": "update", "table": table, "key": key or {}, "values": values or {}})

    def delete(self, table, key):
        operations.append({"type": "delete", "table": table, "key": key or {}})

plan = Plan()
source = ` + string(quotedSource) + `

try:
    globals_dict = {"plan": plan, "context": context}
    exec(compile(source, "<data-script>", "exec"), globals_dict, globals_dict)
except Exception:
    traceback.print_exc(file=sys.stderr)
    sys.exit(1)

sys.stdout.write(json.dumps({"operations": operations}))`
}

func buildPHPRuntime(source string) string {
	clean := strings.ReplaceAll(source, "<?php", "")
	clean = strings.ReplaceAll(clean, "?>", "")
	return `<?php
$context = json_decode(getenv('NIAS_SCRIPT_CONTEXT') ?: '{}', true);
$operations = [];

class PlanHelper {
    private $operations;

    public function __construct(&$operations) {
        $this->operations = &$operations;
    }

    public function insert($table, $values) {
        $this->operations[] = ["type" => "insert", "table" => $table, "values" => $values ?: []];
    }

    public function update($table, $key, $values) {
        $this->operations[] = ["type" => "update", "table" => $table, "key" => $key ?: [], "values" => $values ?: []];
    }

    public function delete($table, $key) {
        $this->operations[] = ["type" => "delete", "table" => $table, "key" => $key ?: []];
    }
}

$plan = new PlanHelper($operations);

try {
` + indentLines(clean, 4) + `
    echo json_encode(["operations" => $operations], JSON_UNESCAPED_UNICODE | JSON_UNESCAPED_SLASHES);
} catch (Throwable $e) {
    fwrite(STDERR, $e->__toString());
    exit(1);
}`
}

func normalizeRuntimeOperations(ops []scriptRuntimeOperation) ([]rawScriptOperation, error) {
	result := make([]rawScriptOperation, 0, len(ops))
	for _, op := range ops {
		opType := firstNonEmpty(strings.ToLower(strings.TrimSpace(op.Type)), strings.ToLower(strings.TrimSpace(op.OpType)))
		tableName := strings.TrimSpace(firstNonEmpty(op.Table, op.TableName))
		key := firstMap(op.Key, op.PK, op.Where)
		values := firstMap(op.Values, op.After, op.Data)
		switch opType {
		case "insert":
			if tableName == "" || len(values) == 0 {
				return nil, fmt.Errorf("insert operations require table and values")
			}
			result = append(result, rawScriptOperation{OpType: "insert", TableName: tableName, PK: map[string]any{}, After: values})
		case "update":
			if tableName == "" || len(key) == 0 || len(values) == 0 {
				return nil, fmt.Errorf("update operations require table, key, and values")
			}
			result = append(result, rawScriptOperation{OpType: "update", TableName: tableName, PK: key, After: values})
		case "delete":
			if tableName == "" || len(key) == 0 {
				return nil, fmt.Errorf("delete operations require table and key")
			}
			result = append(result, rawScriptOperation{OpType: "delete", TableName: tableName, PK: key, After: map[string]any{}})
		default:
			return nil, fmt.Errorf("unsupported operation type: %s", opType)
		}
	}
	return result, nil
}

func firstMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return map[string]any{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func indentLines(source string, spaces int) string {
	padding := strings.Repeat(" ", spaces)
	lines := strings.Split(source, "\n")
	for i := range lines {
		if strings.TrimSpace(lines[i]) == "" {
			lines[i] = ""
			continue
		}
		lines[i] = padding + lines[i]
	}
	return strings.Join(lines, "\n")
}

func trimRuntimeError(msg string) string {
	msg = strings.TrimSpace(msg)
	if len(msg) > 500 {
		return strings.TrimSpace(msg[:500])
	}
	return msg
}
