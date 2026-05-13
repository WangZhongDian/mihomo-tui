package ui

import (
	"context"

	"github.com/rivo/tview"
)

// Page 带生命周期管理的页面接口
type Page interface {
	tview.Primitive
	Stop()
}

// pageWrapper 包装 tview.Primitive 并附加生命周期管理
type pageWrapper struct {
	tview.Primitive
	ctx    context.Context
	cancel context.CancelFunc
}

// Stop 停止页面，取消所有后台 goroutine
func (p *pageWrapper) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}
