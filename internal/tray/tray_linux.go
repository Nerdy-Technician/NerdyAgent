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
	updateAvail   bool
	lastSessionAt string
}

func (it *sniItem) ContextMenu(x, y int32) *dbus.Error {
	_ = x
	_ = y
	return nil
}

func (it *sniItem) Activate(x, y int32) *dbus.Error {
	_ = x
	_ = y
	openPopup(popupURL("/"))
	return nil
}

func (it *sniItem) SecondaryActivate(x, y int32) *dbus.Error {
	return it.Activate(x, y)
}

func (it *sniItem) Scroll(delta int32, orientation string) *dbus.Error {
	_ = delta
	_ = orientation
	return nil
}

type menuItem struct {
	id        int32
	label     string
	typ       string
	enabled   bool
	visible   bool
	toggle    int32 // -1 none, 0 off, 1 on
	toggleTyp string
	onClick   func()
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

func (m *dbusMenu) rebuild(v view) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rev++
	notifyState := int32(0)
	if v.NotifyPref {
		notifyState = 1
	}
	m.items = []menuItem{
		{id: 1, label: "NerdyRMM Agent", enabled: false, visible: true},
		{id: 2, label: v.Status, enabled: false, visible: true},
		{id: 3, typ: "separator", visible: true},
		{id: 4, label: "View connection properties…", enabled: true, visible: true, onClick: func() {
			openPopup(popupURL("/connection"))
		}},
		{id: 5, label: "View About", enabled: true, visible: true, onClick: func() {
			openPopup(popupURL("/#about"))
		}},
		{id: 6, label: "Notify when a technician connects", enabled: true, visible: true, toggle: notifyState, toggleTyp: "checkmark", onClick: func() {
			p := loadPrefs()
			p.NotifyTechnicianConnect = !p.NotifyTechnicianConnect
			_ = savePrefs(p)
		}},
		{id: 7, label: "Restart agent", enabled: true, visible: true, onClick: func() {
			if err := restartAgent(v.Service); err != nil {
				_ = exec.Command("notify-send", "-a", "NerdyRMM Agent", "-i", "nerdyrmm-agent",
					"Could not restart agent", err.Error()).Start()
				return
			}
			_ = exec.Command("notify-send", "-a", "NerdyRMM Agent", "-i", "nerdyrmm-agent",
				"NerdyRMM Agent", "Agent service restart requested. The tray stays running.").Start()
		}},
		{id: 8, label: "Open web UI", enabled: v.WebURL != "", visible: true, onClick: func() {
			openURL(v.WebURL)
		}},
		{id: 9, typ: "separator", visible: true},
		{id: 10, label: "Quit tray", enabled: true, visible: true, onClick: func() {
			os.Exit(0)
		}},
	}
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
	if it.toggleTyp != "" {
		p["toggle-type"] = dbus.MakeVariant(it.toggleTyp)
		p["toggle-state"] = dbus.MakeVariant(it.toggle)
	}
	return p
}

// Run shows a Cinnamon/AppIndicator-friendly StatusNotifier tray icon plus a
// localhost popup UI (opened on left-click). Requires a user graphical session.
func Run() error {
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return fmt.Errorf("no DISPLAY/WAYLAND_DISPLAY; tray must run in the user graphical session")
	}
	lock, err := acquireSingleton()
	if err != nil {
		return err
	}
	if lock != nil {
		defer lock.Close()
	}
	installUserIcons()

	if _, err := startUI(); err != nil {
		fmt.Fprintf(os.Stderr, "NerdyRMM tray: popup UI failed: %v\n", err)
	} else {
		printUIURL()
	}

	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("session bus: %w", err)
	}
	defer conn.Close()

	busName := "org.nerdyrmm.StatusNotifierItem"
	reply, err := conn.RequestName(busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("tray already running")
	}

	item := &sniItem{conn: conn, menu: newMenu()}
	v := loadView()
	item.online = v.Online
	item.updateAvail = v.UpdateAvail
	if snapLast := lastSessionStamp(v); snapLast != "" {
		item.lastSessionAt = snapLast
	}
	w, h, pix := iconPixmap(v.Online, v.UpdateAvail)
	pm := []pixmap{{Width: w, Height: h, Data: pix}}
	itemSpec := map[string]map[string]*prop.Prop{
		itemIface: {
			"Category":            {Value: "SystemServices", Writable: false, Emit: prop.EmitTrue},
			"Id":                  {Value: "nerdyrmm-agent", Writable: false, Emit: prop.EmitTrue},
			"Title":               {Value: "NerdyRMM Agent", Writable: false, Emit: prop.EmitTrue},
			"Status":              {Value: "Active", Writable: false, Emit: prop.EmitTrue},
			"WindowId":            {Value: int32(0), Writable: false, Emit: prop.EmitTrue},
			"IconName":            {Value: iconThemeName(), Writable: false, Emit: prop.EmitTrue},
			"IconPixmap":          {Value: pm, Writable: false, Emit: prop.EmitTrue},
			"OverlayIconName":     {Value: "", Writable: false, Emit: prop.EmitTrue},
			"OverlayIconPixmap":   {Value: []pixmap{}, Writable: false, Emit: prop.EmitTrue},
			"AttentionIconName":   {Value: "", Writable: false, Emit: prop.EmitTrue},
			"AttentionIconPixmap": {Value: []pixmap{}, Writable: false, Emit: prop.EmitTrue},
			"AttentionMovieName":  {Value: "", Writable: false, Emit: prop.EmitTrue},
			"ToolTip":             {Value: toolTip{Title: "NerdyRMM Agent", Description: v.Tooltip, IconPixmap: pm}, Writable: false, Emit: prop.EmitTrue},
			"ItemIsMenu":          {Value: false, Writable: false, Emit: prop.EmitTrue},
			"Menu":                {Value: dbus.ObjectPath(menuPath), Writable: false, Emit: prop.EmitTrue},
			"IconThemePath":       {Value: "", Writable: false, Emit: prop.EmitTrue},
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
		obj := conn.Object(watcherName, watcherPath)
		call := obj.Call(watcherName+".RegisterStatusNotifierItem", 0, busName)
		if call.Err != nil {
			call = obj.Call(watcherName+".RegisterStatusNotifierItem", 0, busName+itemPath)
		}
		return call.Err
	}
	if err := register(); err != nil {
		fmt.Fprintf(os.Stderr, "NerdyRMM tray: waiting for StatusNotifierWatcher: %v\n", err)
	}

	fmt.Println("NerdyRMM tray started (StatusNotifier + localhost popup)")
	ticker := time.NewTicker(2 * time.Second)
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
	changed := v.Online != it.online || v.UpdateAvail != it.updateAvail
	it.online = v.Online
	it.updateAvail = v.UpdateAvail
	it.maybeNotifyLocked(v)
	it.mu.Unlock()
	it.menu.rebuild(v)
	w, h, pix := iconPixmap(v.Online, v.UpdateAvail)
	pm := []pixmap{{Width: w, Height: h, Data: pix}}
	if it.props != nil {
		_ = it.props.Set(itemIface, "Title", dbus.MakeVariant("NerdyRMM Agent"))
		_ = it.props.Set(itemIface, "IconName", dbus.MakeVariant(iconThemeName()))
		_ = it.props.Set(itemIface, "IconPixmap", dbus.MakeVariant(pm))
		_ = it.props.Set(itemIface, "ToolTip", dbus.MakeVariant(toolTip{
			Title:       "NerdyRMM Agent",
			Description: v.Tooltip,
			IconPixmap:  pm,
		}))
		if v.UpdateAvail {
			_ = it.props.Set(itemIface, "Status", dbus.MakeVariant("NeedsAttention"))
		} else {
			_ = it.props.Set(itemIface, "Status", dbus.MakeVariant("Active"))
		}
	}
	if changed {
		_ = it.conn.Emit(itemPath, itemIface+".NewIcon")
		st := "Active"
		if v.UpdateAvail {
			st = "NeedsAttention"
		}
		_ = it.conn.Emit(itemPath, itemIface+".NewStatus", st)
	}
	_ = it.conn.Emit(itemPath, itemIface+".NewToolTip")
	_ = it.conn.Emit(menuPath, menuIface+".LayoutUpdated", it.menu.rev, int32(0))
}

func (it *sniItem) maybeNotifyLocked(v view) {
	stamp := lastSessionStamp(v)
	if stamp == "" || stamp == it.lastSessionAt {
		return
	}
	it.lastSessionAt = stamp
	if v.LastSession == nil || v.LastSession.Kind != "open" {
		return
	}
	if !loadPrefs().NotifyTechnicianConnect {
		return
	}
	notifyTechnician(v.LastSession.Type)
	if it.conn != nil {
		_ = it.conn.Emit(itemPath, itemIface+".NewStatus", "NeedsAttention")
	}
}

func lastSessionStamp(v view) string {
	if v.LastSession == nil {
		return ""
	}
	return v.LastSession.At + "|" + v.LastSession.Kind + "|" + v.LastSession.Type
}
