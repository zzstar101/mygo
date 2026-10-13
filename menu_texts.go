package mygo

import "github.com/egoist/mygo/i18n"

// MenuTexts are the labels the items of a menu take from their role
// (MenuItem.Role), in one language: what an item says when the app gives it
// no Label. The labels that name the app, as "Quit Notes", write it with
// the app's name.
//
// Items say them in the language of the locale the app shows
// (App.SetLocale), and an item whose label is its role's says the new
// language's when the locale changes. UseMenuTexts changes them or adds
// languages. A translation leaves what it does not change at its zero
// value, which keeps what the language it is for says, or what English
// says.
type MenuTexts struct {
	// The items of the Edit menu.
	Undo, Redo, Cut, Copy, Paste, PasteAndMatchStyle, Delete, SelectAll string
	// The items of the View menu: Reload, Force Reload, Toggle Developer
	// Tools, Actual Size, Zoom In, Zoom Out and Toggle Full Screen.
	Reload, ForceReload, ToggleDevTools, ActualSize, ZoomIn, ZoomOut, ToggleFullScreen string
	// The items of the Window menu: Minimize, Zoom, and Bring All to
	// Front. Close closes a document's window, and CloseWindow a window.
	Minimize, Zoom, BringAllToFront, Close, CloseWindow string
	// The items of the File menu of an app of documents. Open, SaveAs,
	// Rename and MoveTo end with an ellipsis, as they ask for more.
	// RevertToSaved is macOS's Revert.
	New, Open, OpenRecent, Save, SaveAs, Duplicate, Rename, MoveTo, Revert, RevertToSaved string
	// QuitApp, About and Hide write the items of the app menu of macOS,
	// with the app's name: "Quit Notes", "About Notes", "Hide Notes".
	QuitApp, About, Hide func(app string) string
	// Quit and Exit are the item that quits the app on Linux, and on
	// Windows.
	Quit, Exit string
	// HideOthers, ShowAll, Services, StartSpeaking and StopSpeaking are
	// the rest of the items of macOS's app menu and its Edit menu.
	HideOthers, ShowAll, Services, StartSpeaking, StopSpeaking string
	// The menus of the menu bar.
	File, Edit, View, Format, Window, Help string
	// The items of the Format menu.
	Bold, Italic, Underline, Strikethrough, AlignLeft, Center, AlignRight string
}

// builtinMenuTexts are the labels of the roles, in the languages the
// framework has them in, which are those of the controls of package ui.
var builtinMenuTexts = map[string]MenuTexts{
	"en": {
		Undo: "Undo", Redo: "Redo", Cut: "Cut", Copy: "Copy", Paste: "Paste",
		PasteAndMatchStyle: "Paste and Match Style", Delete: "Delete", SelectAll: "Select All",
		Reload: "Reload", ForceReload: "Force Reload", ToggleDevTools: "Toggle Developer Tools",
		ActualSize: "Actual Size", ZoomIn: "Zoom In", ZoomOut: "Zoom Out", ToggleFullScreen: "Toggle Full Screen",
		Minimize: "Minimize", Zoom: "Zoom", BringAllToFront: "Bring All to Front", Close: "Close", CloseWindow: "Close Window",
		New: "New", Open: "Open…", OpenRecent: "Open Recent", Save: "Save", SaveAs: "Save As…",
		Duplicate: "Duplicate", Rename: "Rename…", MoveTo: "Move To…", Revert: "Revert", RevertToSaved: "Revert to Saved",
		QuitApp: func(app string) string { return "Quit " + app },
		About:   func(app string) string { return "About " + app },
		Hide:    func(app string) string { return "Hide " + app },
		Quit:    "Quit", Exit: "Exit",
		HideOthers: "Hide Others", ShowAll: "Show All", Services: "Services",
		StartSpeaking: "Start Speaking", StopSpeaking: "Stop Speaking",
		File: "File", Edit: "Edit", View: "View", Format: "Format", Window: "Window", Help: "Help",
		Bold: "Bold", Italic: "Italic", Underline: "Underline", Strikethrough: "Strikethrough",
		AlignLeft: "Align Left", Center: "Center", AlignRight: "Align Right",
	},
	"zh-Hans": {
		Undo: "撤销", Redo: "重做", Cut: "剪切", Copy: "复制", Paste: "粘贴",
		PasteAndMatchStyle: "粘贴并匹配样式", Delete: "删除", SelectAll: "全选",
		Reload: "重新载入", ForceReload: "强制重新载入", ToggleDevTools: "切换开发者工具",
		ActualSize: "实际大小", ZoomIn: "放大", ZoomOut: "缩小", ToggleFullScreen: "切换全屏幕",
		Minimize: "最小化", Zoom: "缩放", BringAllToFront: "前置全部窗口", Close: "关闭", CloseWindow: "关闭窗口",
		New: "新建", Open: "打开…", OpenRecent: "打开最近使用", Save: "保存", SaveAs: "另存为…",
		Duplicate: "创建副本", Rename: "重新命名…", MoveTo: "移到…", Revert: "还原", RevertToSaved: "还原到上次存储的版本",
		QuitApp: func(app string) string { return "退出" + app },
		About:   func(app string) string { return "关于" + app },
		Hide:    func(app string) string { return "隐藏" + app },
		Quit:    "退出", Exit: "退出",
		HideOthers: "隐藏其他", ShowAll: "全部显示", Services: "服务",
		StartSpeaking: "开始讲话", StopSpeaking: "停止讲话",
		File: "文件", Edit: "编辑", View: "显示", Format: "格式", Window: "窗口", Help: "帮助",
		Bold: "粗体", Italic: "斜体", Underline: "下划线", Strikethrough: "删除线",
		AlignLeft: "左对齐", Center: "居中", AlignRight: "右对齐",
	},
	"zh-Hant": {
		Undo: "復原", Redo: "重做", Cut: "剪下", Copy: "複製", Paste: "貼上",
		PasteAndMatchStyle: "貼上並符合樣式", Delete: "刪除", SelectAll: "全選",
		Reload: "重新載入", ForceReload: "強制重新載入", ToggleDevTools: "切換開發者工具",
		ActualSize: "實際大小", ZoomIn: "放大", ZoomOut: "縮小", ToggleFullScreen: "切換全螢幕",
		Minimize: "最小化", Zoom: "縮放", BringAllToFront: "將全部視窗移至最前", Close: "關閉", CloseWindow: "關閉視窗",
		New: "新增", Open: "開啟…", OpenRecent: "打開最近使用過的檔案", Save: "儲存", SaveAs: "另存新檔…",
		Duplicate: "製作副本", Rename: "重新命名…", MoveTo: "移至…", Revert: "回復", RevertToSaved: "回復至上次儲存的版本",
		QuitApp: func(app string) string { return "結束" + app },
		About:   func(app string) string { return "關於" + app },
		Hide:    func(app string) string { return "隱藏" + app },
		Quit:    "結束", Exit: "結束",
		HideOthers: "隱藏其他", ShowAll: "顯示全部", Services: "服務",
		StartSpeaking: "開始朗讀", StopSpeaking: "停止朗讀",
		File: "檔案", Edit: "編輯", View: "顯示方式", Format: "格式", Window: "視窗", Help: "輔助說明",
		Bold: "粗體", Italic: "斜體", Underline: "底線", Strikethrough: "刪除線",
		AlignLeft: "靠左對齊", Center: "置中", AlignRight: "靠右對齊",
	},
}

// menuTexts are the labels of the roles, in every language the framework
// has them in, as the app changed them. A locale of another language says
// English.
var menuTexts = i18n.New("en", builtinMenuTexts)

// UseMenuTexts changes the labels the items of menus take from their role,
// or adds the labels of languages the framework has none of. A language's
// labels are its tag's, as "zh-Hant-TW": its own language's with the
// changes the app made, where a field left at its zero value keeps what
// the language says.
//
//	mygo.UseMenuTexts(map[string]mygo.MenuTexts{
//		"de": {Undo: "Widerrufen", Redo: "Wiederholen", Edit: "Bearbeiten"},
//	})
//
// It is safe from any goroutine, makes its changes at once, and the menus
// of the app and of its windows say the labels it set.
func UseMenuTexts(texts map[string]MenuTexts) {
	menuTexts.Change(texts)
	postMain(relabelMenus)
}

// roleTexts returns the labels of the roles in the language of the locale
// the app shows. Safe from any goroutine.
func roleTexts() *MenuTexts {
	return menuTexts.Get(App.localeAnyThread())
}

// relabelMenus gives the items of the menus in use whose label is their
// role's the label of the locale the app shows, and installs the menus
// again. Main thread only.
func relabelMenus() {
	if !App.IsReady() {
		return // the menus take their labels as Run installs them
	}
	app := App.Menu()
	menuMu.Lock()
	changed := app != nil && app.relabelLocked()
	var windows []*Window
	for _, w := range Windows() {
		if w.menu != nil && w.native != nil && w.menu.relabelLocked() {
			windows = append(windows, w)
		}
	}
	menuMu.Unlock()
	if changed {
		backend().SetApplicationMenu(app.snapshot())
	}
	for _, w := range windows {
		w.native.SetMenu(w.menu.snapshot())
	}
}

// relabelLocked gives the items of the menu, and of its submenus, whose
// label is their role's the role's label now, and reports whether one
// changed. menuMu must be held.
func (m *Menu) relabelLocked() bool {
	changed := false
	for _, it := range m.items {
		if it.Role != "" && it.roleLabel != "" && it.Label == it.roleLabel {
			if label, _ := roleDefaults(it.Role); label != it.Label {
				it.Label, it.roleLabel = label, label
				changed = true
			}
		}
		if it.sub != nil {
			changed = it.sub.relabelLocked() || changed
		}
	}
	return changed
}

// localeAnyThread returns the locale the app shows, from any goroutine: the
// one the windows last heard of, else SetLocale's, else the user's
// preferred language.
func (a *Application) localeAnyThread() string {
	if v, _ := a.shownLocale.Load().(string); v != "" {
		return v
	}
	return a.localeInEffect()
}
