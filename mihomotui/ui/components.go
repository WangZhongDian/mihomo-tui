package ui

import (
	"fmt"

	"github.com/rivo/tview"
)

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
