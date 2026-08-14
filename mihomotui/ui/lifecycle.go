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

// Shower 可选接口：页面每次被切换为可见时调用（用于刷新数据，避免显示过期缓存）。
type Shower interface {
	OnShow()
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
