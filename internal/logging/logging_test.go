package logging

import (
	"encoding/json"
	"errors"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func encodeLogstash(t *testing.T, fields ...zapcore.Field) map[string]interface{} {
	t.Helper()
	enc := NewLogstashEncoder(zapcore.NewJSONEncoder(zapcore.EncoderConfig{MessageKey: "msg"}))
	buf, err := enc.EncodeEntry(zapcore.Entry{Message: "Error processing events"}, fields)
	if err != nil {
		t.Fatalf("EncodeEntry: %v", err)
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("invalid JSON %q: %v", buf.String(), err)
	}
	return out
}

func assertErrorObject(t *testing.T, out map[string]interface{}, wantMsg string) {
	t.Helper()
	obj, ok := out["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected \"error\" to be an object, got %T (%v)", out["error"], out["error"])
	}
	if obj["message"] != wantMsg {
		t.Fatalf("error.message = %v, want %q", obj["message"], wantMsg)
	}
}

func TestLogstashEncoderErrorFieldIsObject(t *testing.T) {
	msg := `event missing critical fields: event_type="", instance_id=""`
	out := encodeLogstash(t, zap.String("type", "ec2-hook"), zap.Error(errors.New(msg)))
	assertErrorObject(t, out, msg)
	if out["type"] != "ec2-hook" || out["artifactId"] != "slashsre-devops-controller" {
		t.Fatalf("other fields lost: %v", out)
	}
}

func TestLogstashEncoderStringErrorFieldIsObject(t *testing.T) {
	out := encodeLogstash(t, zap.String("error", "not found"))
	assertErrorObject(t, out, "not found")
}

func TestLogstashEncoderDoesNotMutateFields(t *testing.T) {
	fields := []zapcore.Field{zap.Error(errors.New("boom"))}
	encodeLogstash(t, fields...)
	if fields[0].Type != zapcore.ErrorType {
		t.Fatalf("caller fields were modified: %+v", fields[0])
	}
}

func TestLogstashEncoderNilError(t *testing.T) {
	out := encodeLogstash(t, zap.Error(nil))
	if _, ok := out["error"]; ok {
		t.Fatalf("nil error should be skipped, got %v", out["error"])
	}
}
