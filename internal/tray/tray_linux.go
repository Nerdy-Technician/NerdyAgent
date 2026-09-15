//go:build linux

package tray

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

const (
	watcherName = "org.kde.StatusNotifierWatcher"
	watcherPath = "/StatusNotifierWatcher"
	itemPath    = "/StatusNotifierItem"
	menuPath    = "/MenuBar"
	itemIface   = "org.kde.StatusNotifierItem"
	menuIface   = "com.canonical.dbusmenu"
)

type pixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

type toolTip struct {
	IconName    string
	IconPixmap  []pixmap
	Title       string
	Description string
}

type sniItem struct {
	conn          *dbus.Conn
	props         *prop.Properties
	menu          *dbusMenu
	mu            sync.Mutex
	online        bool
	fingerprint   string
	offlineStreak int
	registered    bool
	watcherOwner  string
}

func (it *sniItem) ContextMenu(x, y int32) *dbus.Error {
	_ = x
	_ = y
	return nil
}

func (it *sniItem) Activate(x, y int32) *dbus.Error {
	_ = x
	_ = y
	return nil
}

func (it *sniItem) SecondaryActivate(x, y int32) *dbus.Error {
	_ = x
	_ = y
	return nil
}

func (it *sniItem) Scroll(delta int32, orientation string) *dbus.Error {
	_ = delta
	_ = orientation
	return nil
}

type menuItem struct {
	id      int32
	label   string
	typ     string
	enabled bool
	visible bool
	onClick func()
}

type dbusMenu struct {
	rev   uint32
	items []menuItem
	mu    sync.Mutex
}

func newMenu() *dbusMenu {
	m := &dbusMenu{}
	m.rebuild(loadView())
	return m
}

func (m *dbusMenu) rebuild(v view) bool {
	items := []menuItem{
		{id: 1, label: v.Title, enabled: false, visible: true},
		{id: 2, label: v.Status, enabled: false, visible: true},
		{id: 3, typ: "separator", visible: true},
		{id: 4, label: "Open documentation", enabled: true, visible: true, onClick: func() {
			_ = exec.Command("xdg-open", v.DocsURL).Start()
		}},
		{id: 5, label: "Open status file", enabled: true, visible: true, onClick: func() {
			if v.StatusFn != "" {
				_ = exec.Command("xdg-open", v.StatusFn).Start()
			}
		}},
		{id: 6, typ: "separator", visible: true},
		{id: 7, label: "Quit tray", enabled: true, visible: true, onClick: func() {
			os.Exit(0)
		}},
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if menuLabelsEqual(m.items, items) {
		m.items = items
		return false
	}
	m.rev++
	m.items = items
	return true
}

func (m *dbusMenu) GetLayout(parentID int32, recursionDepth int32, propertyNames []string) (uint32, menuLayout, *dbus.Error) {
	_ = parentID
	_ = recursionDepth
	_ = propertyNames
	m.mu.Lock()
	defer m.mu.Unlock()
	children := make([]dbus.Variant, 0, len(m.items))
	for _, it := range m.items {
		children = append(children, dbus.MakeVariant(menuLayout{
			ID:         it.id,
			Properties: itemProps(it),
			Children:   []dbus.Variant{},
		}))
	}
	root := menuLayout{
		ID:         0,
		Properties: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")},
		Children:   children,
	}
	return m.rev, root, nil
}

func (m *dbusMenu) GetGroupProperties(ids []int32, propertyNames []string) ([]menuProperties, *dbus.Error) {
	_ = propertyNames
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]menuProperties, 0, len(ids))
	for _, id := range ids {
		for _, it := range m.items {
			if it.id == id {
				out = append(out, menuProperties{ID: id, Properties: itemProps(it)})
			}
		}
	}
	return out, nil
}

func (m *dbusMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, it := range m.items {
		if it.id == id {
			if v, ok := itemProps(it)[name]; ok {
				return v, nil
			}
		}
	}
	return dbus.MakeVariant(""), nil
}

func (m *dbusMenu) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	_ = data
	_ = timestamp
	if eventID != "clicked" {
		return nil
	}
	m.mu.Lock()
	var fn func()
	for _, it := range m.items {
		if it.id == id {
			fn = it.onClick
			break
		}
	}
	m.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}

func (m *dbusMenu) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, ev := range events {
		_ = m.Event(ev.ID, ev.EventID, ev.Data, ev.Timestamp)
	}
	return nil, nil
}

func (m *dbusMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	_ = id
	m.rebuild(loadView())
	return true, nil
}

func (m *dbusMenu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	_ = ids
	m.rebuild(loadView())
	return nil, nil, nil
}

type menuLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

type menuProperties struct {
	ID         int32
	Properties map[string]dbus.Variant
}

type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

func itemProps(it menuItem) map[string]dbus.Variant {
	p := map[string]dbus.Variant{
		"visible": dbus.MakeVariant(it.visible),
		"enabled": dbus.MakeVariant(it.enabled),
	}
	if it.typ == "separator" {
		p["type"] = dbus.MakeVariant("separator")
		return p
	}
	p["label"] = dbus.MakeVariant(it.label)
	return p
}

func menuLabelsEqual(a, b []menuItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].id != b[i].id || a[i].label != b[i].label || a[i].typ != b[i].typ {
			return false
		}
	}
	return true
}

// Run shows a Cinnamon/AppIndicator-friendly StatusNotifier tray icon.
// It reads status.json written by the agent service and never opens config.json
// secrets. Requires a user graphical session (DISPLAY + session D-Bus), not root systemd.
func Run() error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return fmt.Errorf("no DISPLAY/WAYLAND_DISPLAY; tray must run in the user graphical session")
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}
	defer conn.Close()

	busName := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("tray bus name already taken")
	}

	item := &sniItem{conn: conn, menu: newMenu()}
	v := loadView()
	item.online = v.Online
	item.fingerprint = v.fingerprint()
	absPNG, themePath, err := InstallThemeIcons()
	if err != nil {
		fmt.Fprintf(os.Stderr, "NerdyRMM tray: icon install: %v\n", err)
	}
	name := iconFile(v.Online)
	if name == "" {
		name = absPNG
	}
	if name == "" {
		name = iconName
	}
	pm := sniPixmaps(v.Online)
	itemSpec := map[string]map[string]*prop.Prop{
		itemIface: {
			"Category":            {Value: "SystemServices", Writable: false, Emit: prop.EmitTrue},
			"Id":                  {Value: iconName, Writable: false, Emit: prop.EmitTrue},
			"Title":               {Value: v.Title, Writable: false, Emit: prop.EmitTrue},
			"Status":              {Value: "Active", Writable: false, Emit: prop.EmitTrue},
			"WindowId":            {Value: int32(0), Writable: false, Emit: prop.EmitTrue},
			"IconName":            {Value: name, Writable: false, Emit: prop.EmitTrue},
			"IconPixmap":          {Value: pm, Writable: false, Emit: prop.EmitTrue},
			"OverlayIconName":     {Value: "", Writable: false, Emit: prop.EmitTrue},
			"OverlayIconPixmap":   {Value: []pixmap{}, Writable: false, Emit: prop.EmitTrue},
			"AttentionIconName":   {Value: "", Writable: false, Emit: prop.EmitTrue},
			"AttentionIconPixmap": {Value: []pixmap{}, Writable: false, Emit: prop.EmitTrue},
			"AttentionMovieName":  {Value: "", Writable: false, Emit: prop.EmitTrue},
			"ToolTip":             {Value: toolTip{Title: v.Title, Description: v.tooltipText(), IconPixmap: pm}, Writable: false, Emit: prop.EmitTrue},
			"ItemIsMenu":          {Value: true, Writable: false, Emit: prop.EmitTrue},
			"Menu":                {Value: dbus.ObjectPath(menuPath), Writable: false, Emit: prop.EmitTrue},
			"IconThemePath":       {Value: themePath, Writable: false, Emit: prop.EmitTrue},
		},
	}
	menuSpec := map[string]map[string]*prop.Prop{
		menuIface: {
			"Version":       {Value: uint32(3), Writable: false, Emit: prop.EmitTrue},
			"TextDirection": {Value: "ltr", Writable: false, Emit: prop.EmitTrue},
			"Status":        {Value: "normal", Writable: false, Emit: prop.EmitTrue},
			"IconThemePath": {Value: []string{}, Writable: false, Emit: prop.EmitTrue},
		},
	}

	props, err := prop.Export(conn, itemPath, itemSpec)
	if err != nil {
		return err
	}
	if _, err := prop.Export(conn, menuPath, menuSpec); err != nil {
		return err
	}
	item.props = props
	if err := conn.Export(item, itemPath, itemIface); err != nil {
		return err
	}
	if err := conn.Export(item.menu, menuPath, menuIface); err != nil {
		return err
	}

	register := func() error {
		owner := nameOwner(conn, watcherName)
		if owner != "" && owner == item.watcherOwner && item.registered {
			return nil
		}
		obj := conn.Object(watcherName, watcherPath)
		call := obj.Call(watcherName+".RegisterStatusNotifierItem", 0, busName)
		if call.Err != nil {
			call = obj.Call(watcherName+".RegisterStatusNotifierItem", 0, busName+itemPath)
		}
		if call.Err == nil {
			item.registered = true
			item.watcherOwner = owner
			if item.watcherOwner == "" {
				item.watcherOwner = nameOwner(conn, watcherName)
			}
		} else {
			item.registered = false
		}
		return call.Err
	}
	if err := register(); err != nil {
		fmt.Fprintf(os.Stderr, "NerdyRMM tray: waiting for StatusNotifierWatcher: %v\n", err)
	}

	fmt.Println("NerdyRMM tray started (StatusNotifier / AppIndicator)")
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	regTick := time.NewTicker(15 * time.Second)
	defer regTick.Stop()

	for {
		select {
		case <-ticker.C:
			item.refresh()
		case <-regTick.C:
			_ = register()
		}
	}
}

func (it *sniItem) refresh() {
	v := loadView()
	it.mu.Lock()
	// Require two consecutive offline samples before flipping online→offline so a
	// transient status.json rewrite cannot flicker the icon/badge.
	if v.Online {
		it.offlineStreak = 0
	} else if it.online {
		it.offlineStreak++
		if it.offlineStreak < 2 {
			it.mu.Unlock()
			return
		}
	}
	fp := v.fingerprint()
	iconChanged := v.Online != it.online
	same := fp == it.fingerprint
	it.online = v.Online
	it.fingerprint = fp
	it.mu.Unlock()
	if same {
		return
	}

	menuChanged := it.menu.rebuild(v)
	pm := sniPixmaps(v.Online)
	name := iconFile(v.Online)
	if name == "" {
		name = iconName
	}
	if it.props != nil {
		_ = it.props.Set(itemIface, "Title", dbus.MakeVariant(v.Title))
		if iconChanged {
			_ = it.props.Set(itemIface, "IconName", dbus.MakeVariant(name))
			_ = it.props.Set(itemIface, "IconPixmap", dbus.MakeVariant(pm))
		}
		_ = it.props.Set(itemIface, "ToolTip", dbus.MakeVariant(toolTip{
			Title:       v.Title,
			Description: v.tooltipText(),
			IconPixmap:  pm,
		}))
	}
	if iconChanged {
		_ = it.conn.Emit(itemPath, itemIface+".NewIcon")
	}
	_ = it.conn.Emit(itemPath, itemIface+".NewToolTip")
	if menuChanged {
		it.menu.mu.Lock()
		rev := it.menu.rev
		it.menu.mu.Unlock()
		_ = it.conn.Emit(menuPath, menuIface+".LayoutUpdated", rev, int32(0))
	}
}

func nameOwner(conn *dbus.Conn, name string) string {
	var owner string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner); err != nil {
		return ""
	}
	return owner
}

func sniPixmaps(online bool) []pixmap {
	descs := iconPixmaps(online)
	out := make([]pixmap, 0, len(descs))
	for _, d := range descs {
		out = append(out, pixmap{Width: d.Width, Height: d.Height, Data: d.ARGB})
	}
	return out
}
