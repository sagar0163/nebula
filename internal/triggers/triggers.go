package triggers

import (
	"context"
	"fmt"
	"time"
	"strings"
	"strconv"

	"github.com/fsnotify/fsnotify"
)

type Trigger interface {
	Start(ctx context.Context, action func())
	Stop()
}

type CronTrigger struct {
	Schedule string
	ticker   *time.Ticker
	done     chan bool
}

func NewCronTrigger(schedule string) *CronTrigger {
	return &CronTrigger{Schedule: schedule, done: make(chan bool)}
}

func parseDuration(sched string) time.Duration {
	// Simple fallback parser for "1h", "30m", etc.
	d, err := time.ParseDuration(sched)
	if err == nil {
		return d
	}
	// Very naive natural language handling
	if strings.HasPrefix(sched, "every ") {
		parts := strings.Split(sched, " ")
		if len(parts) >= 3 {
			val, _ := strconv.Atoi(parts[1])
			if parts[2] == "minutes" || parts[2] == "minute" {
				return time.Duration(val) * time.Minute
			}
			if parts[2] == "hours" || parts[2] == "hour" {
				return time.Duration(val) * time.Hour
			}
			if parts[2] == "days" || parts[2] == "day" {
				return time.Duration(val) * 24 * time.Hour
			}
		}
	}
	// Default to 1 hour if unparseable
	return time.Hour
}

func (c *CronTrigger) Start(ctx context.Context, action func()) {
	d := parseDuration(c.Schedule)
	c.ticker = time.NewTicker(d)
	go func() {
		for {
			select {
			case <-c.done:
				return
			case <-c.ticker.C:
				action()
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (c *CronTrigger) Stop() {
	if c.ticker != nil {
		c.ticker.Stop()
	}
	c.done <- true
}

type FileWatchTrigger struct {
	Path    string
	watcher *fsnotify.Watcher
	done    chan bool
}

func NewFileWatchTrigger(path string) *FileWatchTrigger {
	return &FileWatchTrigger{Path: path, done: make(chan bool)}
}

func (f *FileWatchTrigger) Start(ctx context.Context, action func()) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Println("Error creating fsnotify watcher:", err)
		return
	}
	f.watcher = watcher

	err = watcher.Add(f.Path)
	if err != nil {
		fmt.Println("Error adding path to watcher:", err)
		return
	}

	go func() {
		defer watcher.Close()
		// debounce channel to avoid triggering on every single rapid save
		debounceTimer := time.NewTimer(100 * time.Millisecond)
		debounceTimer.Stop()
		pendingTrigger := false

		for {
			select {
			case <-f.done:
				return
			case <-ctx.Done():
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
					pendingTrigger = true
					debounceTimer.Reset(2 * time.Second)
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				fmt.Println("Watcher error:", err)
			case <-debounceTimer.C:
				if pendingTrigger {
					pendingTrigger = false
					action()
				}
			}
		}
	}()
}

func (f *FileWatchTrigger) Stop() {
	if f.watcher != nil {
		_ = f.watcher.Close()
	}
	f.done <- true
}
