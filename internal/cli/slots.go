package cli

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/naoray/anvil/internal/agentapi"
	"github.com/naoray/anvil/internal/checks"
	"github.com/naoray/anvil/internal/config"
	"github.com/naoray/anvil/internal/git"
	"github.com/naoray/anvil/internal/resources"
	"github.com/naoray/anvil/internal/slots"
	"github.com/naoray/anvil/internal/ui"
)

var slotsCmd = &cobra.Command{
	Use:   "slots",
	Short: "Show machine-wide slots for expensive checks (read-only)",
	Long: `Show the machine-wide semaphore that expensive checks share across every
project and worktree: capacity, the 1-minute load average and its limit, the
runs holding slots and the queue of waiting checks in admission order.

A check takes slots when it is 'expensive: true' (weight 1) or declares
'slots: N' (weight N). Capacity and load gating are configured in the global
config (~/.config/anvil/anvil.yaml):

  checks:
    slots: 2         # default: max(2, CPUs/4)
    max_load: 6.0    # wait while the 1-minute load average exceeds this
    load_wait: 2m    # then proceed with a warning (default 2m)

ANVIL_CHECK_SLOTS, ANVIL_CHECK_MAX_LOAD and ANVIL_CHECK_LOAD_WAIT override
those settings. Waiters are admitted strictly in order: higher
'anvil check --priority' first, then arrival.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runAgentCommand(cmd, agentCommand{Name: "slots", Human: printSlotsHuman}, func() (agentResult, error) {
			manager, err := slotManager(nil)
			if err != nil {
				return agentResult{}, err
			}
			status, err := manager.Snapshot()
			data := slotsData{Dir: manager.Dir, Status: status}
			if err != nil {
				return agentResult{Data: data, Warnings: []string{err.Error()}}, nil
			}
			return agentResult{Data: data}, nil
		})
	},
}

type slotsData struct {
	Dir string `json:"dir"`
	slots.Status
}

// checkSlotInfo is a gated check's slot weight with the machine-wide
// summary and this check's own holders and tickets.
type checkSlotInfo struct {
	Weight    int                `json:"weight"`
	Capacity  int                `json:"capacity"`
	InUse     int                `json:"in_use"`
	Queued    *int               `json:"queued"`
	Holders   []slots.Holder     `json:"holders"`
	Queue     []slots.QueueEntry `json:"queue"`
	QueueBusy bool               `json:"queue_busy"`
}

func newCheckSlotInfo(check config.CheckConfig, status slots.Status) *checkSlotInfo {
	weight := checks.SlotWeight(check)
	if weight == 0 {
		return nil
	}
	queued := len(status.Queue)
	info := &checkSlotInfo{Weight: weight, Capacity: status.Capacity, InUse: status.InUse, Queued: &queued,
		Holders: []slots.Holder{}, Queue: []slots.QueueEntry{}, QueueBusy: status.QueueBusy}
	if status.QueueBusy {
		info.Queued, info.Holders, info.Queue = nil, nil, nil // unavailable, not empty
	}
	for _, holder := range status.Holders {
		if holder.Check == check.Name {
			info.Holders = append(info.Holders, holder)
		}
	}
	for _, entry := range status.Queue {
		if entry.Check == check.Name {
			info.Queue = append(info.Queue, entry)
		}
	}
	return info
}

// worktreeSlots is the machine-wide slot summary with one worktree's own
// holders and queued tickets (anvil status --json).
type worktreeSlots struct {
	Capacity  int                `json:"capacity"`
	InUse     int                `json:"in_use"`
	Queued    *int               `json:"queued"`
	Holders   []slots.Holder     `json:"holders"`
	Queue     []slots.QueueEntry `json:"queue"`
	QueueBusy bool               `json:"queue_busy"`
}

func describeWorktreeSlots(wenv *worktreeEnv, warnings *[]string) *worktreeSlots {
	manager, err := slotManager(wenv.PC.GlobalConfig)
	if err != nil {
		*warnings = append(*warnings, err.Error())
		return nil
	}
	status, err := manager.Snapshot()
	if err != nil {
		*warnings = append(*warnings, "check slots: "+err.Error())
	}
	id := ""
	if wenv.Identity != nil {
		id = wenv.Identity.ID
	}
	mine := func(worktreeID, path string) bool {
		if id != "" && worktreeID != "" {
			return worktreeID == id
		}
		return git.PathsEqual(path, wenv.Worktree.Path)
	}
	queued := len(status.Queue)
	out := &worktreeSlots{Capacity: status.Capacity, InUse: status.InUse, Queued: &queued, Holders: []slots.Holder{}, Queue: []slots.QueueEntry{},
		QueueBusy: status.QueueBusy}
	if status.QueueBusy {
		out.Queued, out.Holders, out.Queue = nil, nil, nil // unavailable, not empty
	}
	for _, holder := range status.Holders {
		if mine(holder.WorktreeID, holder.Worktree) {
			out.Holders = append(out.Holders, holder)
		}
	}
	for _, entry := range status.Queue {
		if mine(entry.WorktreeID, entry.Worktree) {
			out.Queue = append(out.Queue, entry)
		}
	}
	return out
}

// slotManager resolves the machine-wide slot settings. A nil global config
// is loaded from disk.
func slotManager(global *config.GlobalConfig) (*slots.Manager, error) {
	if global == nil {
		loaded, err := config.LoadOrCreateGlobalConfig()
		if err != nil {
			return nil, agentapi.Wrap(agentapi.CodeConfigInvalid, err)
		}
		global = loaded
	}
	settings, err := slots.ResolveSettings(global.Checks, os.Getenv)
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeConfigInvalid, fmt.Errorf("check slot settings: %w", err))
	}
	root, err := resources.Root()
	if err != nil {
		return nil, agentapi.Wrap(agentapi.CodeInternal, err)
	}
	return slots.New(slots.Dir(root), settings), nil
}

// checkSlotOptions prepares slot admission for the selected checks. Weights
// that can never fit fail before anything runs.
func checkSlotOptions(cmd *cobra.Command, wenv *worktreeEnv, selected []config.CheckConfig, warn func(string)) (checks.SlotOptions, error) {
	priority, err := cmd.Flags().GetInt("priority")
	if err != nil {
		return checks.SlotOptions{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	wait, err := cmd.Flags().GetDuration("slot-wait")
	if err != nil {
		return checks.SlotOptions{}, agentapi.Wrap(agentapi.CodeInvalidArguments, err)
	}
	if wait < 0 {
		return checks.SlotOptions{}, agentapi.Errorf(agentapi.CodeInvalidArguments, "--slot-wait must not be negative (0 waits until interrupted)")
	}
	var gated *config.CheckConfig
	for i := range selected {
		if checks.SlotWeight(selected[i]) > 0 && gated == nil {
			gated = &selected[i]
		}
	}
	if gated == nil {
		return checks.SlotOptions{}, nil
	}
	// A nested gated check waits for capacity its ancestor holds; at full
	// capacity an unbounded wait never ends, so require an explicit bound.
	if holder := os.Getenv(checks.SlotHolderEnv); holder != "" && wait == 0 {
		return checks.SlotOptions{}, agentapi.Errorf(agentapi.CodeConfigInvalid,
			"check %q needs a machine-wide slot but is running inside check %q, which already holds one; nested slot-gated checks can deadlock. Pass --slot-wait to bound the wait, or do not nest expensive checks",
			gated.Name, holder)
	}
	manager, err := slotManager(wenv.PC.GlobalConfig)
	if err != nil {
		return checks.SlotOptions{}, err
	}
	for _, check := range selected {
		if weight := checks.SlotWeight(check); weight > manager.Capacity {
			capErr := &slots.CapacityError{Check: check.Name, Weight: weight, Capacity: manager.Capacity}
			return checks.SlotOptions{}, agentapi.Wrap(agentapi.CodeConfigInvalid, capErr)
		}
	}
	return checks.SlotOptions{Manager: manager, Priority: priority, Wait: wait, Warn: warn}, nil
}

// slotProgress prints queue progress to stderr (never into JSON).
func slotProgress(check string) func(slots.Progress) {
	return func(progress slots.Progress) {
		fmt.Fprintf(os.Stderr, "check %s: waiting for %s (position %d of %d)\n", check, progress.WaitingFor, progress.Position, progress.Queued)
	}
}

// slotRunError classifies slot admission failures (nil when err is not one).
func slotRunError(err error) error {
	var busy *slots.BusyError
	var capErr *slots.CapacityError
	switch {
	case errors.As(err, &busy):
		return (&agentapi.Error{Code: agentapi.CodeResourceBusy, Message: busy.Error(), Cause: err}).WithDetails(map[string]any{
			"waiting_for": busy.WaitingFor, "position": busy.Position, "queued": busy.Queued,
			"capacity": busy.Capacity, "in_use": busy.InUse, "holders": busy.Holders, "queue_busy": busy.QueueBusy,
		})
	case errors.As(err, &capErr):
		return &agentapi.Error{Code: agentapi.CodeConfigInvalid, Message: capErr.Error(), Cause: err}
	}
	return nil
}

func printSlotsHuman(result agentResult) error {
	data, ok := result.Data.(slotsData)
	if !ok {
		return nil
	}
	load := "unsupported"
	if data.Load.Value != nil {
		load = fmt.Sprintf("%.2f", *data.Load.Value)
	}
	limit := "off"
	if data.Load.Max > 0 {
		limit = fmt.Sprintf("%.2f", data.Load.Max)
	}
	ui.PrintInfo(fmt.Sprintf("Slots: %d of %d in use; load %s (max %s)", data.InUse, data.Capacity, load, limit))
	for _, holder := range data.Holders {
		fmt.Printf("  slot %d  %-16s pid %-7d %s (since %s)\n", holder.Slot, holder.Check, holder.PID, holder.Worktree, holder.Since.Format(time.RFC3339))
	}
	for _, entry := range data.Queue {
		fmt.Printf("  #%d %-4s %-16s pid %-7d priority %d weight %d %s\n", entry.Position, entry.WaitingFor, entry.Check, entry.PID, entry.Priority, entry.Weight, entry.Worktree)
	}
	return nil
}

func init() {
	addJSONFlag(slotsCmd, agentCommand{Name: "slots", Human: printSlotsHuman})
	rootCmd.AddCommand(slotsCmd)
}
