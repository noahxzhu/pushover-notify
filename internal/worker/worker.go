package worker

import (
	"context"
	"log/slog"
	"time"

	"github.com/noahxzhu/pushover-notify/internal/model"
	"github.com/noahxzhu/pushover-notify/internal/pushover"
	"github.com/noahxzhu/pushover-notify/internal/storage"
)

type Worker struct {
	store      *storage.Store
	client     *pushover.Client
	updateChan chan struct{}
	onUpdate   func() // Callback when notifications are updated
}

func NewWorker(store *storage.Store) *Worker {
	return &Worker{
		store:      store,
		client:     &pushover.Client{},
		updateChan: make(chan struct{}, 1),
	}
}

// SetOnUpdate sets a callback function that will be called when notifications are updated
func (w *Worker) SetOnUpdate(fn func()) {
	w.onUpdate = fn
}

// Refresh signals the worker to re-evaluate the schedule immediately
func (w *Worker) Refresh() {
	select {
	case w.updateChan <- struct{}{}:
	default:
		// Channel already has a pending signal, no need to block
	}
}

func (w *Worker) Start(ctx context.Context) {
	slog.Info("Worker started (Event-Driven)")

	timer := time.NewTimer(time.Hour) // Initial long duration
	timer.Stop()                      // Stop immediately, we'll reset it

	for {
		// 1. Process due items and calculate next run time
		nextRun := w.checkAndProcess()

		// 2. Set timer
		now := time.Now()
		var duration time.Duration

		if nextRun.IsZero() {
			// No pending items, wait indefinitely (via updateChan)
			// effectively stop timer
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			slog.Info("No pending notifications. Worker idle.")
		} else {
			duration = nextRun.Sub(now)
			if duration < 0 {
				duration = 0 // Run immediately
			}

			// Reset timer
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(duration)
			slog.Info("Next check scheduled", "in", duration, "at", nextRun.Format("15:04:05"))
		}

		// 3. Wait for event
		select {
		case <-ctx.Done():
			slog.Info("Worker stopped")
			return
		case <-w.updateChan:
			slog.Info("Worker received update signal. Refreshing...")
			// Continue loop -> re-check
		case <-timer.C:
			// Timer fired -> Continue loop -> re-check
		}
	}
}

// checkAndProcess sends due notifications and returns the time of the NEXT scheduled event
func (w *Worker) checkAndProcess() time.Time {
	settings := w.store.GetSettings()

	// If credentials missing, we can't send
	if settings.PushoverToken == "" || settings.PushoverUser == "" {
		return time.Time{} // Return zero to idle
	}

	w.client.Token = settings.PushoverToken
	w.client.User = settings.PushoverUser

	pending := w.store.GetPending()
	now := time.Now()
	saveNeeded := false

	var earliestNext time.Time

	for _, n := range pending {
		// Use per-notification settings
		repeatInterval, err := time.ParseDuration(n.RepeatInterval)
		if err != nil {
			repeatInterval = 30 * time.Minute
		}

		repeatTimes := n.RepeatTimes
		if repeatTimes == 0 {
			repeatTimes = 3
		}

		// Calculate when this notification SHOULD be sent next
		var nextSendTime time.Time

		if n.SendsCount == 0 {
			nextSendTime = n.ScheduledTime.Truncate(time.Minute)
		} else {
			// Calculate next time based on original scheduled time + intervals
			// This ensures all repeats are at XX:XX:00
			nextSendTime = n.ScheduledTime.Truncate(time.Minute).Add(repeatInterval * time.Duration(n.SendsCount))
		}

		// Check if it's due now (or past due)
		if !now.Before(nextSendTime) {
			// IT IS DUE
			if n.SendsCount < repeatTimes {
				delay := now.Sub(nextSendTime)
				slog.Info("Sending notification", "content", n.Content, "attempt", n.SendsCount+1, "max", repeatTimes, "scheduled", nextSendTime.Format("15:04:05"), "delay", delay)
				err := w.client.SendMessage("Reminder", n.Content)
				if err != nil {
					slog.Error("Failed to send pushover message", "error", err)
					// Update LastPushTime even on failure to avoid spamming
					n.LastPushTime = now
					saveNeeded = true
				} else {
					n.SendsCount++
					n.LastPushTime = now
					saveNeeded = true
				}
			}

			if n.SendsCount >= repeatTimes {
				n.Status = model.StatusDone
				saveNeeded = true
				slog.Info("Notification marked as Done", "id", n.ID)
			} else {
				// Calculate NEXT time for this item after processing
				// Use scheduled time + intervals to keep at XX:XX:00
				nextForThis := n.ScheduledTime.Truncate(time.Minute).Add(repeatInterval * time.Duration(n.SendsCount))
				if earliestNext.IsZero() || nextForThis.Before(earliestNext) {
					earliestNext = nextForThis
				}
			}
		} else {
			// Not due yet, track its next time
			if earliestNext.IsZero() || nextSendTime.Before(earliestNext) {
				earliestNext = nextSendTime
			}
		}
	}

	if saveNeeded {
		if err := w.store.Save(); err != nil {
			slog.Error("Failed to save store", "error", err)
		} else if w.onUpdate != nil {
			w.onUpdate()
		}
	}

	return earliestNext
}
