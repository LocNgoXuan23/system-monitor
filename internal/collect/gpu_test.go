package collect

import "testing"

func TestMergeGPUProcs(t *testing.T) {
	in := []GPUProcSample{
		{PID: 100, VRAM: 200 << 20, Graphics: true},
		{PID: 200, VRAM: 3 << 30, Compute: true},
		{PID: 100, VRAM: 100 << 20, Compute: true}, // same PID, other context type
		{PID: 200, VRAM: 1 << 30, Compute: true},   // same PID, second GPU
	}
	got := MergeGPUProcs(in)
	if len(got) != 2 {
		t.Fatalf("len=%d want 2, got %+v", len(got), got)
	}
	// Sorted by VRAM descending: 200 holds 4 GiB summed across two GPUs.
	if got[0].PID != 200 || got[0].VRAM != 4<<30 || got[0].Type != "C" {
		t.Errorf("got[0]=%+v", got[0])
	}
	// 100 appears in both lists, so it holds both context types.
	if got[1].PID != 100 || got[1].VRAM != 300<<20 || got[1].Type != "C+G" {
		t.Errorf("got[1]=%+v", got[1])
	}
}

func TestMergeGPUProcsValueNotAvailable(t *testing.T) {
	// NVML reports 0xFFFFFFFFFFFFFFFF when per-process VRAM is unavailable
	// (notably under MIG). Rendering it verbatim would show 16 EiB.
	got := MergeGPUProcs([]GPUProcSample{{PID: 7, VRAM: ^uint64(0), Compute: true}})
	if len(got) != 1 || got[0].VRAM != 0 {
		t.Errorf("got=%+v, want one row with VRAM 0", got)
	}
}

func TestMergeGPUProcsTieBreakByPID(t *testing.T) {
	// Equal VRAM must order by PID ascending, so rows do not swap between ticks.
	got := MergeGPUProcs([]GPUProcSample{
		{PID: 9, VRAM: 1 << 20, Compute: true},
		{PID: 3, VRAM: 1 << 20, Compute: true},
	})
	if len(got) != 2 || got[0].PID != 3 || got[1].PID != 9 {
		t.Errorf("got=%+v, want PIDs [3 9]", got)
	}
}

func TestMergeGPUProcsEmpty(t *testing.T) {
	if got := MergeGPUProcs(nil); len(got) != 0 {
		t.Errorf("got=%+v, want empty", got)
	}
}
