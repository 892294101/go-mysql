package canal

import (
	"fmt"
	"github.com/pingcap/errors"
	"sync/atomic"
	"time"

	"github.com/892294101/go-mysql/mysql"
	"github.com/892294101/go-mysql/replication"
	"github.com/892294101/parser/ast"
	"github.com/google/uuid"
)

var (
	ALTER  = "ALTER"
	CREATE = "CREATE"
	DROP   = "DROP"
)

const (
	DropTempTab       = "^DROP\\s+(\\/\\*\\!40005\\s+)?TEMPORARY\\s+(\\*\\/\\s+)?TABLE"
	DropGlobalTempTab = "^DROP\\s+(\\/\\*\\!40005\\s+)?GLOBAL\\s+TEMPORARY\\s+(\\*\\/\\s+)?TABLE"
)

func (c *Canal) startSyncer() (*replication.BinlogStreamer, error) {

	gset := c.master.GTIDSet()
	if gset == nil || gset.String() == "" {
		pos := c.master.Position()
		s, err := c.syncer.StartSync(pos)
		if err != nil {
			return nil, errors.Errorf("start sync replication at binlog %v error %v", pos, err)
		}
		c.cfg.Logger.Infof("start sync binlog at binlog file %v", pos)
		return s, nil
	} else {
		gsetClone := gset.Clone()
		s, err := c.syncer.StartSyncGTID(gset)
		if err != nil {
			return nil, errors.Errorf("start sync replication at GTID set %v error %v", gset, err)
		}
		c.cfg.Logger.Infof("start sync binlog at GTID set %v", gsetClone)
		return s, nil
	}
}

func (c *Canal) runSyncBinlog() error {
	s, err := c.startSyncer()
	if err != nil {
		return err
	}

	savePos := false
	force := false

	// The name of the binlog file received in the fake rotate event.
	// It must be preserved until the new position is saved.
	fakeRotateLogName := ""

	for {
		ev, err := s.GetEvent(c.ctx)

		if err != nil {
			return errors.Trace(err)
		}

		// Update the delay between the Canal and the Master before the handler hooks are called
		c.updateReplicationDelay(ev)

		// If log pos equals zero then the received event is a fake rotate event and
		// contains only a name of the next binlog file
		// See https://github.com/mysql/mysql-server/blob/8e797a5d6eb3a87f16498edcb7261a75897babae/sql/rpl_binlog_sender.h#L235
		// and https://github.com/mysql/mysql-server/blob/8cc757da3d87bf4a1f07dcfb2d3c96fed3806870/sql/rpl_binlog_sender.cc#L899
		if ev.Header.LogPos == 0 {
			switch e := ev.Event.(type) {
			case *replication.RotateEvent:
				fakeRotateLogName = string(e.NextLogName)
				c.cfg.Logger.Infof("received fake rotate event, next log name is %s", e.NextLogName)
			}

			continue
		}

		savePos = false
		force = false
		pos := c.master.Position()
		pos.Pos = ev.Header.LogPos

		// new file name received in the fake rotate event
		if fakeRotateLogName != "" {
			pos.Name = fakeRotateLogName
		}

		// We only save position with RotateEvent and XIDEvent.
		// For RowsEvent, we can't save the position until meeting XIDEvent
		// which tells the whole transaction is over.
		// TODO: If we meet any DDL query, we must save too.
		switch e := ev.Event.(type) {
		case *replication.RotateEvent:
			pos.Name = string(e.NextLogName)
			pos.Pos = uint32(e.Position)
			c.cfg.Logger.Infof("rotate binlog to %s", pos)
			savePos = true
			force = true
			if err = c.eventHandler.OnRotate(e); err != nil {
				return errors.Trace(err)
			}
		case *replication.RowsEvent:
			err = c.handleRowsEvent(ev)
			if err != nil {
				return errors.Trace(err)

			}
		case *replication.XIDEvent:
			savePos = true
			// try to save the position later
			if err := c.eventHandler.OnXID(pos,e); err != nil {
				return errors.Trace(err)
			}
			if e.GSet != nil {
				c.master.UpdateGTIDSet(e.GSet)
			}
		case *replication.MariadbGTIDEvent:
			// try to save the GTID later
			gtid, err := mysql.ParseMariadbGTIDSet(e.GTID.String())
			if err != nil {
				return errors.Trace(err)
			}
			if err := c.eventHandler.OnGTID(gtid); err != nil {
				return errors.Trace(err)
			}
		case *replication.GTIDEvent:
			u, _ := uuid.FromBytes(e.SID)
			gtid, err := mysql.ParseMysqlGTIDSet(fmt.Sprintf("%s:%d", u.String(), e.GNO))
			if err != nil {
				return errors.Trace(err)
			}
			if err := c.eventHandler.OnGTID(gtid); err != nil {
				return errors.Trace(err)
			}
		case *replication.QueryEvent:
			// 过滤不要的drop操作（过滤drop历史表和全局临时表）
			if FilterOther(e.Query) {
				c.cfg.Logger.Errorf("parse query(%s), This event will be skipped normally", e.Query)
				continue
			}

			stmts, _, err := c.parser.Parse(string(e.Query), "", "")
			if err != nil {
				c.cfg.Logger.Errorf("parse query(%s) err %v, This event will be skipped by exception", e.Query, err)
				continue
			}
			for _, stmt := range stmts {
				// +++++++++++++++++++++ TABLE DDL(DROP VIEW、Rename Table、Alter Table、Drop Table、Create Table、Truncate Table)
				nodes := parseStmt(stmt)
				switch vNodes := nodes.(type) {
				case []*node:
					for _, node := range vNodes {
						if node.db == "" {
							node.db = string(e.Schema)
						}
					}
					if len(vNodes) > 0 {
						savePos = true
						force = true
						switch tabSmt := stmt.(type) {
						case *ast.CreateTableStmt, *ast.AlterTableStmt, *ast.DropTableStmt, *ast.RenameTableStmt, *ast.TruncateTableStmt:
							if err = c.eventHandler.OnTableDDL(pos, e, tabSmt); err != nil {
								return errors.Trace(err)
							}
						}
					}

				}
				switch tabSmt := stmt.(type) {
				// +++++++++++++++++++++ DATABASE DDL(CREATE DATABASE、ALTER DATABASE、DROP DATABASE)
				case *ast.CreateDatabaseStmt, *ast.DropDatabaseStmt, *ast.AlterDatabaseStmt:
					savePos = true
					force = true

					if err = c.eventHandler.OnDataBaseDDL(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}
				// +++++++++++++++++++++ INDEX DDL(CREATE INDEX、DROP INDEX)
				case *ast.CreateIndexStmt, *ast.DropIndexStmt:
					savePos = true
					force = true
					if err = c.eventHandler.OnIndexDDL(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}
				// +++++++++++++++++++++ VIEW DDL(CREATE VIEW)
				case *ast.CreateViewStmt:
					savePos = true
					force = true
					if err = c.eventHandler.OnViewDDL(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}
				// +++++++++++++++++++++ INDEX DDL(CREATE SEQUENCE、DROP SEQUENCE、ALTER SEQUENCE)
				case *ast.CreateSequenceStmt, *ast.DropSequenceStmt, *ast.AlterSequenceStmt:
					savePos = true
					force = true
					if err = c.eventHandler.OnSequenceDDL(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}
				// +++++++++++++++++++++ USER ROLE DDL(CREATE USER、ALTER USER、DROP USER、RENAME USER、GRANT PROXY、GRANT ROLE、GRANT STMT)
				case *ast.CreateUserStmt, *ast.AlterUserStmt, *ast.DropUserStmt, *ast.RenameUserStmt:
					savePos = true
					force = true
					if err = c.eventHandler.OnUserDDL(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}
				case *ast.GrantProxyStmt, *ast.GrantRoleStmt, *ast.GrantStmt:
					savePos = true
					force = true
					if err = c.eventHandler.OnGrantDDL(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}
				case *ast.BeginStmt, *ast.CommitStmt:
					savePos = true
					force = true
					if err = c.eventHandler.OnTransaction(pos, e, tabSmt); err != nil {
						return errors.Trace(err)
					}

				}

			}
			if savePos && e.GSet != nil {
				c.master.UpdateGTIDSet(e.GSet)
			}
		default:
			continue
		}

		if savePos {
			c.master.Update(pos)
			c.master.UpdateTimestamp(ev.Header.Timestamp)
			fakeRotateLogName = ""
			if err := c.eventHandler.OnPosSynced(pos, c.master.GTIDSet(), force); err != nil {
				return errors.Trace(err)
			}
		}
	}
}

// create table,  rename table, alter table. drop table, truncate table
type node struct {
	db    string
	table string
}

func parseStmt(stmt ast.StmtNode) (res interface{}) {
	var ns []*node
	switch t := stmt.(type) {
	// +++++++++++++++++++++ TABLE DDL
	case *ast.RenameTableStmt:
		for _, tableInfo := range t.TableToTables {
			n := &node{
				db:    tableInfo.OldTable.Schema.String(),
				table: tableInfo.OldTable.Name.String(),
			}
			ns = append(ns, n)
		}
	case *ast.AlterTableStmt:
		n := &node{
			db:    t.Table.Schema.String(),
			table: t.Table.Name.String(),
		}
		ns = []*node{n}
	case *ast.DropTableStmt:
		for _, table := range t.Tables {
			n := &node{
				db:    table.Schema.String(),
				table: table.Name.String(),
			}
			ns = append(ns, n)
		}
	case *ast.CreateTableStmt:
		n := &node{
			db:    t.Table.Schema.String(),
			table: t.Table.Name.String(),
		}
		ns = []*node{n}
	case *ast.TruncateTableStmt:
		n := &node{
			db:    t.Table.Schema.String(),
			table: t.Table.Name.String(),
		}
		ns = []*node{n}
	}
	return ns
}

func (c *Canal) updateReplicationDelay(ev *replication.BinlogEvent) {
	var newDelay uint32
	now := uint32(time.Now().Unix())
	if now >= ev.Header.Timestamp {
		newDelay = now - ev.Header.Timestamp
	}
	atomic.StoreUint32(c.delay, newDelay)
}

func (c *Canal) handleRowsEvent(e *replication.BinlogEvent) error {
	ev := e.Event.(*replication.RowsEvent)

	var action string
	switch e.Header.EventType {
	case replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2:
		action = InsertAction
	case replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2:
		action = DeleteAction
	case replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2:
		action = UpdateAction
	default:
		return errors.Errorf("%s not supported now", e.Header.EventType)
	}
	events := newRowsEvent(action, ev, e.Header)
	return c.eventHandler.OnRow(events)
}

func (c *Canal) FlushBinlog() error {
	_, err := c.Execute("FLUSH BINARY LOGS")
	return errors.Trace(err)
}

func (c *Canal) WaitUntilPos(pos mysql.Position, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	for {
		select {
		case <-timer.C:
			return errors.Errorf("wait position %v too long > %s", pos, timeout)
		default:
			err := c.FlushBinlog()
			if err != nil {
				return errors.Trace(err)
			}
			curPos := c.master.Position()
			if curPos.Compare(pos) >= 0 {
				return nil
			} else {
				c.cfg.Logger.Debugf("master pos is %v, wait catching %v", curPos, pos)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}
}

func (c *Canal) GetMasterPos() (mysql.Position, error) {
	rr, err := c.Execute("SHOW MASTER STATUS")
	if err != nil {
		return mysql.Position{}, errors.Trace(err)
	}

	name, _ := rr.GetString(0, 0)
	pos, _ := rr.GetInt(0, 1)

	return mysql.Position{Name: name, Pos: uint32(pos)}, nil
}

func (c *Canal) GetMasterGTIDSet() (mysql.GTIDSet, error) {
	query := ""
	switch c.cfg.Flavor {
	case mysql.MariaDBFlavor:
		query = "SELECT @@GLOBAL.gtid_current_pos"
	default:
		query = "SELECT @@GLOBAL.GTID_EXECUTED"
	}
	rr, err := c.Execute(query)
	if err != nil {
		return nil, errors.Trace(err)
	}
	gx, err := rr.GetString(0, 0)
	if err != nil {
		return nil, errors.Trace(err)
	}
	gset, err := mysql.ParseGTIDSet(c.cfg.Flavor, gx)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return gset, nil
}

func (c *Canal) CatchMasterPos(timeout time.Duration) error {
	pos, err := c.GetMasterPos()
	if err != nil {
		return errors.Trace(err)
	}

	return c.WaitUntilPos(pos, timeout)
}
