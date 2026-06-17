package tray

import (
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
	"github.com/godbus/dbus/v5/prop"
)

const (
	sniInterface      = "org.kde.StatusNotifierItem"
	sniPath           = "/StatusNotifierItem"
	menuInterface     = "com.canonical.dbusmenu"
	menuPath          = "/MenuBar"
	watcherBusName    = "org.kde.StatusNotifierWatcher"
	watcherObjectPath = "/StatusNotifierWatcher"
)

// sniServer implements the KDE StatusNotifierItem / DBusMenu protocol in pure
// Go. It works with any desktop environment that supports SNI (KDE, GNOME
// Shell with the appindicator extension, XFCE, etc.).
type sniServer struct {
	conn     *dbus.Conn
	service  string
	onShow   func()
	onQuit   func()
	props    *prop.Properties
	mu       sync.RWMutex
	iconName string
	title    string
}

func newSNIServer(cfg Config) (*sniServer, error) {
	log.Println("tray: connecting to session bus")
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		log.Printf("tray: session bus connect failed: %v", err)
		return nil, err
	}

	pid := os.Getpid()
	service := fmt.Sprintf("org.kde.StatusNotifierItem-%d-0", pid)
	log.Printf("tray: creating SNI service %s", service)

	s := &sniServer{
		conn:     conn,
		service:  service,
		iconName: cfg.IconName,
		title:    cfg.Tooltip,
		onShow:   cfg.OnShow,
		onQuit:   cfg.OnQuit,
	}
	if s.iconName == "" {
		s.iconName = "gozik"
	}
	if s.title == "" {
		s.title = "Gozik"
	}

	if err := conn.Export(s, sniPath, sniInterface); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Export(s, menuPath, menuInterface); err != nil {
		conn.Close()
		return nil, err
	}
	var node introspect.Node
	if err := xml.Unmarshal([]byte(sniIntrospection()), &node); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Export(introspect.NewIntrospectable(&node), sniPath, introspect.IntrospectData.Name); err != nil {
		conn.Close()
		return nil, err
	}

	s.props = prop.New(conn, sniPath, prop.Map{
		sniInterface: {
			"Category":      {Value: "ApplicationStatus", Writable: false, Emit: prop.EmitTrue},
			"Id":            {Value: "gozik", Writable: false, Emit: prop.EmitTrue},
			"Title":         {Value: s.title, Writable: true, Emit: prop.EmitTrue},
			"Status":        {Value: "Active", Writable: true, Emit: prop.EmitTrue},
			"WindowId":      {Value: uint32(0), Writable: false, Emit: prop.EmitTrue},
			"IconName":      {Value: s.iconName, Writable: true, Emit: prop.EmitTrue},
			"IconThemePath": {Value: "", Writable: false, Emit: prop.EmitTrue},
			"Menu":          {Value: dbus.ObjectPath(menuPath), Writable: false, Emit: prop.EmitTrue},
		},
	})

	reply, err := conn.RequestName(service, dbus.NameFlagDoNotQueue)
	if err != nil {
		log.Printf("tray: RequestName failed: %v", err)
		conn.Close()
		return nil, err
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		log.Printf("tray: RequestName reply %d, not primary owner", reply)
		conn.Close()
		return nil, fmt.Errorf("failed to acquire bus name %s", service)
	}
	log.Printf("tray: acquired bus name %s", service)

	if err := s.registerWithWatcher(); err != nil {
		log.Printf("tray: watcher registration failed: %v", err)
	} else {
		log.Println("tray: registered with StatusNotifierWatcher")
	}

	return s, nil
}

func (s *sniServer) registerWithWatcher() error {
	obj := s.conn.Object(watcherBusName, dbus.ObjectPath(watcherObjectPath))
	return obj.Call(watcherBusName+".RegisterStatusNotifierItem", 0, s.service).Err
}

func (s *sniServer) setStatus(status string) {
	s.props.SetMust(sniInterface, "Status", status)
}

func (s *sniServer) setIconName(name string) {
	s.mu.Lock()
	s.iconName = name
	s.mu.Unlock()
	s.props.SetMust(sniInterface, "IconName", name)
}

func (s *sniServer) close() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

// ContextMenu is called when the user requests the context menu.
func (s *sniServer) ContextMenu(x, y int32) *dbus.Error {
	return nil
}

// Activate is called when the user activates the icon (e.g. left click).
func (s *sniServer) Activate(x, y int32) *dbus.Error {
	if s.onShow != nil {
		s.onShow()
	}
	return nil
}

// SecondaryActivate is called on middle click.
func (s *sniServer) SecondaryActivate(x, y int32) *dbus.Error {
	return nil
}

// Scroll is called on mouse wheel events.
func (s *sniServer) Scroll(delta int32, orientation string) *dbus.Error {
	return nil
}

// GetLayout returns the DBusMenu layout.
func (s *sniServer) GetLayout(parentId int32, recursionDepth int32, propertyNames []string) (uint32, map[string]dbus.Variant, *dbus.Error) {
	return 0, s.menuLayout(), nil
}

// GetGroupProperties returns properties for a group of menu items.
func (s *sniServer) GetGroupProperties(ids []int32, propertyNames []string) ([][2]dbus.Variant, *dbus.Error) {
	var result [][2]dbus.Variant
	for _, id := range ids {
		props := s.menuItemProperties(id)
		result = append(result, [2]dbus.Variant{dbus.MakeVariant(id), dbus.MakeVariant(props)})
	}
	return result, nil
}

// GetProperty returns a single menu item property.
func (s *sniServer) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	props := s.menuItemProperties(id)
	if v, ok := props[name]; ok {
		return v, nil
	}
	return dbus.MakeVariant(""), nil
}

// AboutToShow is called before a submenu is shown.
func (s *sniServer) AboutToShow(id int32) (bool, *dbus.Error) {
	return false, nil
}

// AboutToShowGroup is called before a group of submenus is shown.
func (s *sniServer) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return nil, nil, nil
}

// Event handles menu item activation.
func (s *sniServer) Event(id int32, eventId string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventId == "clicked" {
		switch id {
		case 1:
			if s.onShow != nil {
				s.onShow()
			}
		case 2:
			if s.onQuit != nil {
				s.onQuit()
			}
		}
	}
	return nil
}

// EventGroup handles a batch of menu events.
type menuEvent struct {
	Id        int32
	EventId   string
	Data      dbus.Variant
	Timestamp uint32
}

func (s *sniServer) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, ev := range events {
		_ = s.Event(ev.Id, ev.EventId, ev.Data, ev.Timestamp)
	}
	return nil, nil
}

func (s *sniServer) menuLayout() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"id":               dbus.MakeVariant(int32(0)),
		"type":             dbus.MakeVariant("standard"),
		"label":            dbus.MakeVariant(s.title),
		"children-display": dbus.MakeVariant("submenu"),
		"submenu": dbus.MakeVariant([]dbus.Variant{
			dbus.MakeVariant(s.menuItem(1, "Show")),
			dbus.MakeVariant(s.menuItem(2, "Quit")),
		}),
	}
}

func (s *sniServer) menuItem(id int32, label string) map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"id":      dbus.MakeVariant(id),
		"type":    dbus.MakeVariant("standard"),
		"label":   dbus.MakeVariant(label),
		"enabled": dbus.MakeVariant(true),
		"visible": dbus.MakeVariant(true),
	}
}

func (s *sniServer) menuItemProperties(id int32) map[string]dbus.Variant {
	switch id {
	case 0:
		return map[string]dbus.Variant{
			"id":               dbus.MakeVariant(int32(0)),
			"type":             dbus.MakeVariant("standard"),
			"label":            dbus.MakeVariant(s.title),
			"children-display": dbus.MakeVariant("submenu"),
		}
	case 1:
		return s.menuItem(1, "Show")
	case 2:
		return s.menuItem(2, "Quit")
	default:
		return s.menuItem(id, "")
	}
}

func sniIntrospection() string {
	return `
<node>
  <interface name="org.kde.StatusNotifierItem">
    <method name="ContextMenu"><arg type="i" name="x" direction="in"/><arg type="i" name="y" direction="in"/></method>
    <method name="Activate"><arg type="i" name="x" direction="in"/><arg type="i" name="y" direction="in"/></method>
    <method name="SecondaryActivate"><arg type="i" name="x" direction="in"/><arg type="i" name="y" direction="in"/></method>
    <method name="Scroll"><arg type="i" name="delta" direction="in"/><arg type="s" name="orientation" direction="in"/></method>
    <property name="Category" type="s" access="read"/>
    <property name="Id" type="s" access="read"/>
    <property name="Title" type="s" access="read"/>
    <property name="Status" type="s" access="read"/>
    <property name="WindowId" type="u" access="read"/>
    <property name="IconName" type="s" access="read"/>
    <property name="IconThemePath" type="s" access="read"/>
    <property name="Menu" type="o" access="read"/>
  </interface>
  <interface name="com.canonical.dbusmenu">
    <method name="GetLayout">
      <arg type="i" name="parentId" direction="in"/>
      <arg type="i" name="recursionDepth" direction="in"/>
      <arg type="as" name="propertyNames" direction="in"/>
      <arg type="u" name="revision" direction="out"/>
      <arg type="a(ia{sv})" name="layout" direction="out"/>
    </method>
    <method name="GetGroupProperties">
      <arg type="ai" name="ids" direction="in"/>
      <arg type="as" name="propertyNames" direction="in"/>
      <arg type="a(ia{sv})" name="properties" direction="out"/>
    </method>
    <method name="GetProperty">
      <arg type="i" name="id" direction="in"/>
      <arg type="s" name="name" direction="in"/>
      <arg type="v" name="value" direction="out"/>
    </method>
    <method name="AboutToShow">
      <arg type="i" name="id" direction="in"/>
      <arg type="b" name="needUpdate" direction="out"/>
    </method>
    <method name="AboutToShowGroup">
      <arg type="ai" name="ids" direction="in"/>
      <arg type="ai" name="updatesNeeded" direction="out"/>
      <arg type="ai" name="idErrors" direction="out"/>
    </method>
    <method name="Event">
      <arg type="i" name="id" direction="in"/>
      <arg type="s" name="eventId" direction="in"/>
      <arg type="v" name="data" direction="in"/>
      <arg type="u" name="timestamp" direction="in"/>
    </method>
    <method name="EventGroup">
      <arg type="a(isvu)" name="events" direction="in"/>
      <arg type="ai" name="idErrors" direction="out"/>
    </method>
  </interface>
</node>
`
}
