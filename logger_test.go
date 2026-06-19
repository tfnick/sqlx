package sqlx

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestLoggerLogQuery(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{Output: &buf, Enabled: true}

	l.logQuery("SELECT * FROM users WHERE id=?", []any{1}, 3*time.Millisecond, nil)

	out := buf.String()
	if !strings.Contains(out, "[sqlx]") {
		t.Errorf("expected [sqlx] prefix, got: %s", out)
	}
	if !strings.Contains(out, "SELECT * FROM users WHERE id=?") {
		t.Errorf("expected SQL in output, got: %s", out)
	}
	if !strings.Contains(out, "[1]") {
		t.Errorf("expected args in output, got: %s", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("expected duration in output, got: %s", out)
	}
}

func TestLoggerLogQueryError(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{Output: &buf, Enabled: true}

	l.logQuery("SELECT * FROM users", nil, 1*time.Millisecond, errors.New("connection refused"))

	out := buf.String()
	if !strings.Contains(out, "ERROR") {
		t.Errorf("expected ERROR in output, got: %s", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Errorf("expected error message, got: %s", out)
	}
}

func TestLoggerLogExec(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{Output: &buf, Enabled: true}

	l.logExec("INSERT INTO users (name) VALUES (?)", []any{"张三"}, 2*time.Millisecond, 1, nil)

	out := buf.String()
	if !strings.Contains(out, "[sqlx]") {
		t.Errorf("expected [sqlx] prefix, got: %s", out)
	}
	if !strings.Contains(out, "INSERT INTO users (name) VALUES (?)") {
		t.Errorf("expected SQL in output, got: %s", out)
	}
	if !strings.Contains(out, "affected:1") {
		t.Errorf("expected affected:1, got: %s", out)
	}
}

func TestLoggerLogExecError(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{Output: &buf, Enabled: true}

	l.logExec("UPDATE users SET name=? WHERE id=?", []any{"张三", 1}, 1*time.Millisecond, 0, errors.New("deadlock"))

	out := buf.String()
	if !strings.Contains(out, "ERROR") {
		t.Errorf("expected ERROR in output, got: %s", out)
	}
	if !strings.Contains(out, "deadlock") {
		t.Errorf("expected error message, got: %s", out)
	}
}

func TestLoggerDisabled(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{Output: &buf, Enabled: false}

	l.logQuery("SELECT 1", nil, 1*time.Millisecond, nil)

	if buf.Len() != 0 {
		t.Errorf("expected empty output when disabled, got: %s", buf.String())
	}
}

func TestLoggerNilOutput(t *testing.T) {
	l := &Logger{Output: nil, Enabled: true}

	// Should not panic
	l.logQuery("SELECT 1", nil, 1*time.Millisecond, nil)
	l.logExec("INSERT INTO t VALUES (1)", nil, 1*time.Millisecond, 1, nil)
}

func TestGlobalLogDefaults(t *testing.T) {
	if Log == nil {
		t.Fatal("global Log should not be nil")
	}
	if !Log.Enabled {
		t.Fatal("global Log.Enabled should default to true")
	}
	if Log.Output == nil {
		t.Fatal("global Log.Output should default to os.Stdout")
	}
}

func TestLoggerOutputFormat(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{Output: &buf, Enabled: true}

	l.logQuery("SELECT * FROM users WHERE id=?", []any{1}, 4*time.Millisecond+500*time.Microsecond, nil)

	out := buf.String()
	// Format: [sqlx][4.5ms] SELECT * FROM users WHERE id=? [1]
	expected := "[sqlx]"
	if !strings.HasPrefix(out, expected) {
		t.Errorf("expected prefix %q, got: %s", expected, out)
	}
}
