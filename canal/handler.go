package canal

import (
	"github.com/892294101/go-mysql/mysql"
	"github.com/892294101/go-mysql/replication"
)

type EventHandler interface {
	OnRotate(roateEvent *replication.RotateEvent) error
	// OnTableChanged is called when the table is created, altered, renamed or dropped.
	// You need to clear the associated data like cache with the table.
	// It will be called before OnDDL.
	OnTableChanged(schema string, table string) error
	OnRow(e *RowsEvent) error
	OnXID(nextPos mysql.Position) error
	OnGTID(gtid mysql.GTIDSet) error
	// OnPosSynced Use your own way to sync position. When force is true, sync position immediately.
	OnPosSynced(pos mysql.Position, set mysql.GTIDSet, force bool) error
	String() string

	OnTableDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, tabDDL interface{}) error
	OnDataBaseDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, dbDDL interface{}) error
	OnIndexDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, iDDL interface{}) error
	OnViewDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, vDDL interface{}) error
	OnSequenceDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, sDDL interface{}) error
	OnUserDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, uDDL interface{}) error
	OnGrantDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, gDDL interface{}) error
}

type DummyEventHandler struct{}

func (h *DummyEventHandler) OnTableDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, tabDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnDataBaseDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, dbDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnIndexDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, iDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnViewDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, vDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnSequenceDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, sDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnUserDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, uDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnGrantDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, gDDL interface{}) error {
	return nil
}

func (h *DummyEventHandler) OnRotate(*replication.RotateEvent) error               { return nil }
func (h *DummyEventHandler) OnTableChanged(schema string, table string) error      { return nil }
func (h *DummyEventHandler) OnRow(*RowsEvent) error                                { return nil }
func (h *DummyEventHandler) OnXID(mysql.Position) error                            { return nil }
func (h *DummyEventHandler) OnGTID(mysql.GTIDSet) error                            { return nil }
func (h *DummyEventHandler) OnPosSynced(mysql.Position, mysql.GTIDSet, bool) error { return nil }
func (h *DummyEventHandler) String() string                                        { return "DummyEventHandler" }

// `SetEventHandler` registers the sync handler, you must register your
// own handler before starting Canal.
func (c *Canal) SetEventHandler(h EventHandler) { c.eventHandler = h }
