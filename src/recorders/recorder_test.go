package recorders

import (
	"testing"

	"github.com/bililive-go/bililive-go/src/pipeline"
)

func TestEnsurePlayableWebRTCMP4StageAddsConvertForMKV(t *testing.T) {
	cfg := &pipeline.PipelineConfig{}

	got := ensurePlayableWebRTCMP4Stage(cfg, "webrtc", []string{"room.mkv"})

	if len(got.Stages) != 1 {
		t.Fatalf("stages len = %d, want 1", len(got.Stages))
	}
	if got.Stages[0].Name != pipeline.StageNameConvertMp4 {
		t.Fatalf("stage = %q, want %q", got.Stages[0].Name, pipeline.StageNameConvertMp4)
	}
	if got.Stages[0].Options[pipeline.OptionDeleteSource] != false {
		t.Fatalf("delete_source = %v, want false", got.Stages[0].Options[pipeline.OptionDeleteSource])
	}
}

func TestEnsurePlayableWebRTCMP4StageDoesNotDuplicateConvert(t *testing.T) {
	cfg := &pipeline.PipelineConfig{Stages: []pipeline.StageConfig{{Name: pipeline.StageNameConvertMp4}}}

	got := ensurePlayableWebRTCMP4Stage(cfg, "webrtc", []string{"room.mkv"})

	if len(got.Stages) != 1 {
		t.Fatalf("stages len = %d, want 1", len(got.Stages))
	}
}

func TestEnsurePlayableWebRTCMP4StageIgnoresNonWebRTC(t *testing.T) {
	cfg := &pipeline.PipelineConfig{}

	got := ensurePlayableWebRTCMP4Stage(cfg, "flv", []string{"room.flv"})

	if len(got.Stages) != 0 {
		t.Fatalf("stages len = %d, want 0", len(got.Stages))
	}
}
