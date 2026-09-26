package triggers

import (
	"context"
	"fmt"
	"time"
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

func (c *CronTrigger) Start(ctx context.Context, action func()) {
	// naive 1 hour for demo
	c.ticker = time.NewTicker(time.Hour)
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
	Path string
}

func NewFileWatchTrigger(path string) *FileWatchTrigger {
	return &FileWatchTrigger{Path: path}
}

func (f *FileWatchTrigger) Start(ctx context.Context, action func()) {
	go func() {
		fmt.Println("FileWatchTrigger started on", f.Path)
	}()
}

func (f *FileWatchTrigger) Stop() {}
