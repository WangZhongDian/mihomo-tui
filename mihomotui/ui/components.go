package ui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// ShowOverlay 弹出一个独占式全屏浮层（适用于带输入框/列表的复杂弹窗）。
// 与 ShowModal 不同，ShowOverlay 会临时隐藏当前所有页面，浮层关闭后再恢复，
// 因此天然具备：
// - 背景不透明（下层页面被隐藏，不会透过来）
// - 鼠标/键盘事件不会穿透到下层页面
// - 焦点链是 tview 原生逻辑，输入框可以正常输入
// build 回调接收一个 close 函数，供"取消/完成"按钮调用；返回的 Primitive
// 会被加上边框和标题。initialFocus 可选，指定弹窗打开后聚焦的控件。
func ShowOverlay(app *tview.Application, pages *tview.Pages, name, title string, build func(close func()) (body tview.Primitive, initialFocus tview.Primitive)) {
	lastFocus := app.GetFocus()
	// 记录并隐藏当前可见页面，关闭时恢复。
	hidden := make([]string, 0)
	for _, pageName := range pages.GetPageNames(false) {
		if pageName == name {
			continue
		}
		if pages.HasPage(pageName) {
			pages.HidePage(pageName)
			hidden = append(hidden, pageName)
		}
	}
	closed := false
	close := func() {
		if closed {
			return
		}
		closed = true
		pages.HidePage(name)
		pages.RemovePage(name)
		for _, pageName := range hidden {
			pages.ShowPage(pageName)
		}
		if lastFocus != nil {
			app.SetFocus(lastFocus)
		}
	}
	body, initialFocus := build(close)
	if boxed, ok := body.(interface {
		SetBorder(bool) *tview.Box
		SetTitle(string) *tview.Box
	}); ok {
		boxed.SetBorder(true).SetTitle(" " + title + " ")
	}
	// Esc 关闭。
	if boxed, ok := body.(interface {
		SetInputCapture(func(*tcell.EventKey) *tcell.EventKey) *tview.Box
	}); ok {
		boxed.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
			if event.Key() == tcell.KeyEscape {
				close()
				return nil
			}
			return event
		})
	}
	pages.RemovePage(name)
	pages.AddPage(name, body, true, true)
	if initialFocus != nil {
		app.SetFocus(initialFocus)
	} else {
		app.SetFocus(body)
	}
}

// ShowModal 在指定 Pages 上显示一个模态弹窗
func ShowModal(app *tview.Application, pages *tview.Pages, title, message string, buttons []string, doneFunc func(buttonIndex int, buttonLabel string)) {
	lastFocus := app.GetFocus()
	modal := tview.NewModal().
		SetText(fmt.Sprintf("%s\n\n%s", title, message)).
		AddButtons(buttons).
		SetDoneFunc(func(buttonIndex int, buttonLabel string) {
			pages.HidePage("modal")
			pages.RemovePage("modal")
			if doneFunc != nil {
				doneFunc(buttonIndex, buttonLabel)
			}
			if lastFocus != nil {
				app.SetFocus(lastFocus)
			}
		})
	pages.AddPage("modal", modal, true, true)
	app.SetFocus(modal)
}

// ShowConfirmModal 显示确认弹窗（确认/取消）
func ShowConfirmModal(app *tview.Application, pages *tview.Pages, title, message string, onConfirm func()) {
	ShowModal(app, pages, title, message, []string{"确认", "取消"}, func(buttonIndex int, _ string) {
		if buttonIndex == 0 && onConfirm != nil {
			onConfirm()
		}
	})
}

// ShowAlertModal 显示提示弹窗（仅确认按钮）
func ShowAlertModal(app *tview.Application, pages *tview.Pages, title, message string) {
	ShowModal(app, pages, title, message, []string{"确认"}, nil)
}

// NewPaginationBar 创建分页控制栏（上一页 / 页码 / 下一页）
func NewPaginationBar(app *tview.Application, onPrev, onNext func()) (bar *tview.Flex, pageInfo *tview.TextView) {
	prevBtn := tview.NewButton(" < ")
	prevBtn.SetBorder(false)
	prevBtn.SetSelectedFunc(onPrev)

	nextBtn := tview.NewButton(" > ")
	nextBtn.SetBorder(false)
	nextBtn.SetSelectedFunc(onNext)

	pageInfo = tview.NewTextView().
		SetTextAlign(tview.AlignCenter).
		SetDynamicColors(true)

	bar = tview.NewFlex().
		AddItem(prevBtn, 5, 0, false).
		AddItem(pageInfo, 12, 0, false).
		AddItem(nextBtn, 5, 0, false)
	return bar, pageInfo
}

// NewCard 创建一个带边框的卡片容器
func NewCard(title string, content tview.Primitive) *tview.Flex {
	card := tview.NewFlex().
		AddItem(content, 0, 1, false)
	card.SetBorder(true).
		SetTitle(fmt.Sprintf(" %s ", title)).
		SetTitleAlign(tview.AlignLeft)
	card.SetBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	return card
}
