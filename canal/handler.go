package canal

import (
	"github.com/892294101/go-mysql/mysql"
	"github.com/892294101/go-mysql/replication"
)

type EventHandler interface {
	OnRotate(roateEvent *replication.RotateEvent, rawData []byte) error
	OnRow(e *RowsEvent, rawData []byte) error
	OnXID(nextPos mysql.Position, xid *replication.XIDEvent, rawData []byte) error
	OnGTID(gtid mysql.GTIDSet, rawData []byte) error
	// OnPosSynced Use your own way to sync position. When force is true, sync position immediately.
	OnPosSynced(pos mysql.Position, set mysql.GTIDSet, force bool) error
	String() string

	OnTableDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnDataBaseDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnIndexDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnViewDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnSequenceDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnUserDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnGrantDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
	OnTransaction(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error
}

type DummyEventHandler struct{}

func (h *DummyEventHandler) OnTableDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, tabDDL interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnDataBaseDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, dbDDL interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnIndexDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnViewDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnSequenceDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnUserDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnTransaction(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnGrantDDL(nextPos mysql.Position, queryEvent *replication.QueryEvent, d interface{}, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnRotate(*replication.RotateEvent, []byte) error {
	return nil
}

func (h *DummyEventHandler) OnRow(*RowsEvent, []byte) error {
	return nil
}

func (h *DummyEventHandler) OnXID(pos mysql.Position, xid *replication.XIDEvent, rawData []byte) error {
	return nil
}

func (h *DummyEventHandler) OnGTID(mysql.GTIDSet, []byte) error {
	return nil
}

func (h *DummyEventHandler) OnPosSynced(mysql.Position, mysql.GTIDSet, bool) error {
	return nil
}

func (h *DummyEventHandler) String() string {
	return "DummyEventHandler"
}

// `SetEventHandler` registers the sync handler, you must register your
// own handler before starting Canal.
func (c *Canal) SetEventHandler(h EventHandler) { c.eventHandler = h }
