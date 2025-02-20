// Copyright 2020 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0
package stopintegration

import (
	"context"
	"github.com/newrelic/infrastructure-agent/pkg/integrations/outputhandler/v4/dm/testutils"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/newrelic/infrastructure-agent/internal/agent/cmdchannel"
	"github.com/newrelic/infrastructure-agent/internal/agent/cmdchannel/runintegration"
	"github.com/newrelic/infrastructure-agent/pkg/backend/commandapi"
	"github.com/newrelic/infrastructure-agent/pkg/integrations/execution/v4/integration"
	"github.com/newrelic/infrastructure-agent/pkg/integrations/track"
	"github.com/newrelic/infrastructure-agent/pkg/log"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	l = log.WithComponent("test")
)

func TestHandle_returnsErrorOnMissingName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CC stop-intergation is not supported on Windows")
	}

	h := NewHandler(track.NewTracker(nil), integration.ErrLookup, dm.NewNoopEmitter(), l)

	cmdArgsMissingPID := commandapi.Command{
		Args: []byte(`{ "integration_args": ["nri-process-discovery"] }`),
	}

	err := h.Handle(context.Background(), cmdArgsMissingPID, false)
	require.Error(t, err)
	assert.Equal(t, cmdchannel.NewArgsErr(runintegration.ErrNoIntName).Error(), err.Error())
}

func TestHandle_signalStopProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("CC stop-intergation is not supported on Windows")
	}

	// Given a handler with an stoppables tracker
	tracker := track.NewTracker(nil)
	h := NewHandler(tracker, integration.ErrLookup, dm.NewNoopEmitter(), l)

	// When a process context is tracked
	ctx := context.Background()
	ctx, pidC := tracker.Track(ctx, "nri-process-discovery#", nil)

	proc := exec.CommandContext(ctx, "sleep", "5")

	// And process is started and PID is sent
	waitForProc := make(chan struct{})
	go func() {
		require.NoError(t, proc.Start())
		close(waitForProc)
		pidC <- proc.Process.Pid
	}()

	// And process status is running or stopped
	<-waitForProc
	p, err := process.NewProcess(int32(proc.Process.Pid))
	require.NoError(t, err)
	st, err := p.StatusWithContext(ctx)
	require.NoError(t, err)
	if st[0] != process.Sleep && st[0] != process.Running {
		t.Fatal("sleep command should be either running or sleep, got: ", st)
	}

	// WHEN stop handler receives a cmd for the tracked process
	cmd := commandapi.Command{
		Args: []byte(`{ "integration_name": "nri-process-discovery" }`),
	}
	err = h.Handle(context.Background(), cmd, false)
	require.NoError(t, err)

	// THEN process is stopped
	time.Sleep(100 * time.Millisecond) // let OS update proc status
	st, err = p.StatusWithContext(ctx)
	require.NoError(t, err)
	require.NotEqual(t, "R", st)
}
