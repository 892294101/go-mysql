package canal

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/892294101/go-mysql/client"
	"github.com/892294101/go-mysql/mysql"
	"github.com/892294101/go-mysql/replication"
	"github.com/892294101/parser"
	"github.com/pingcap/errors"
)

// Canal can sync your MySQL data into everywhere, like Elasticsearch, Redis, etc...
// MySQL must open row format for binlog
type Canal struct {
	m sync.Mutex

	cfg *Config

	parser     *parser.Parser
	master     *masterInfo
	dumped     bool
	dumpDoneCh chan struct{}
	syncer     *replication.BinlogSyncer

	eventHandler EventHandler

	connLock sync.Mutex
	conn     *client.Conn

	tableLock          sync.RWMutex
	errorTablesGetTime map[string]time.Time

	tableMatchCache   map[string]bool
	includeTableRegex []*regexp.Regexp
	excludeTableRegex []*regexp.Regexp

	delay *uint32

	ctx    context.Context
	cancel context.CancelFunc
}

// canal will retry fetching unknown table's meta after UnknownTableRetryPeriod
var UnknownTableRetryPeriod = time.Second * time.Duration(10)
var ErrExcludedTable = errors.New("excluded table meta")

func NewCanal(cfg *Config) (*Canal, error) {
	c := new(Canal)
	c.cfg = cfg

	c.ctx, c.cancel = context.WithCancel(context.Background())

	c.dumpDoneCh = make(chan struct{})
	c.eventHandler = &DummyEventHandler{}
	c.parser = parser.New()
	if c.cfg.DiscardNoMetaRowEvent {
		c.errorTablesGetTime = make(map[string]time.Time)
	}
	c.master = &masterInfo{logger: c.cfg.Logger}

	c.delay = new(uint32)

	var err error

	if err = c.prepareSyncer(); err != nil {
		return nil, errors.Trace(err)
	}

	if err := c.checkBinlogRowFormat(); err != nil {
		return nil, errors.Trace(err)
	}

	// init table filter
	if n := len(c.cfg.IncludeTableRegex); n > 0 {
		c.includeTableRegex = make([]*regexp.Regexp, n)
		for i, val := range c.cfg.IncludeTableRegex {
			reg, err := regexp.Compile(val)
			if err != nil {
				return nil, errors.Trace(err)
			}
			c.includeTableRegex[i] = reg
		}
	}

	if n := len(c.cfg.ExcludeTableRegex); n > 0 {
		c.excludeTableRegex = make([]*regexp.Regexp, n)
		for i, val := range c.cfg.ExcludeTableRegex {
			reg, err := regexp.Compile(val)
			if err != nil {
				return nil, errors.Trace(err)
			}
			c.excludeTableRegex[i] = reg
		}
	}

	if c.includeTableRegex != nil || c.excludeTableRegex != nil {
		c.tableMatchCache = make(map[string]bool)
	}

	return c, nil
}

func (c *Canal) GetDelay() uint32 {
	return atomic.LoadUint32(c.delay)
}

// Run will first try to dump all data from MySQL master `mysqldump`,
// then sync from the binlog position in the dump data.
// It will run forever until meeting an error or Canal closed.
func (c *Canal) Run() error {
	return c.run()
}

// RunFrom will sync from the binlog position directly, ignore mysqldump.
func (c *Canal) RunFrom(pos mysql.Position) error {
	c.master.Update(pos)

	return c.Run()
}

func (c *Canal) StartFromGTID(set mysql.GTIDSet) error {
	c.master.UpdateGTIDSet(set)

	return c.Run()
}

func (c *Canal) run() error {
	defer func() {
		c.cancel()
	}()

	c.master.UpdateTimestamp(uint32(time.Now().Unix()))

	if err := c.runSyncBinlog(); err != nil {
		if errors.Cause(err) != context.Canceled {
			c.cfg.Logger.Errorf("canal start sync binlog err: %v", err)
			return errors.Trace(err)
		}
	}

	return nil
}

func (c *Canal) Close() {
	c.cfg.Logger.Infof("closing canal")
	c.m.Lock()
	defer c.m.Unlock()

	c.cancel()
	c.syncer.Close()
	c.connLock.Lock()
	c.conn.Close()
	c.conn = nil
	c.connLock.Unlock()

	_ = c.eventHandler.OnPosSynced(c.master.Position(), c.master.GTIDSet(), true)
}

func (c *Canal) WaitDumpDone() <-chan struct{} {
	return c.dumpDoneCh
}

func (c *Canal) Ctx() context.Context {
	return c.ctx
}

func (c *Canal) checkTableMatch(key string) bool {
	// no filter, return true
	if c.tableMatchCache == nil {
		return true
	}

	c.tableLock.RLock()
	rst, ok := c.tableMatchCache[key]
	c.tableLock.RUnlock()
	if ok {
		// cache hit
		return rst
	}
	matchFlag := false
	// check include
	if c.includeTableRegex != nil {
		for _, reg := range c.includeTableRegex {
			if reg.MatchString(key) {
				matchFlag = true
				break
			}
		}
	}
	// check exclude
	if matchFlag && c.excludeTableRegex != nil {
		for _, reg := range c.excludeTableRegex {
			if reg.MatchString(key) {
				matchFlag = false
				break
			}
		}
	}
	c.tableLock.Lock()
	c.tableMatchCache[key] = matchFlag
	c.tableLock.Unlock()
	return matchFlag
}

// CheckBinlogRowImage checks MySQL binlog row image, must be in FULL, MINIMAL, NOBLOB
func (c *Canal) CheckBinlogRowImage(image string) error {
	// need to check MySQL binlog row image? full, minimal or noblob?
	// now only log
	if c.cfg.Flavor == mysql.MySQLFlavor {
		if res, err := c.Execute(`SHOW GLOBAL VARIABLES LIKE 'binlog_row_image'`); err != nil {
			return errors.Trace(err)
		} else {
			// MySQL has binlog row image from 5.6, so older will return empty
			rowImage, _ := res.GetString(0, 1)
			if rowImage != "" && !strings.EqualFold(rowImage, image) {
				return errors.Errorf("MySQL uses %s binlog row image, but we want %s", rowImage, image)
			}
		}
	}

	return nil
}

func (c *Canal) checkBinlogRowFormat() error {
	res, err := c.Execute(`SHOW GLOBAL VARIABLES LIKE 'binlog_format';`)
	if err != nil {
		return errors.Trace(err)
	} else if f, _ := res.GetString(0, 1); f != "ROW" {
		return errors.Errorf("binlog must ROW format, but %s now", f)
	}

	return nil
}

func (c *Canal) prepareSyncer() error {
	cfg := replication.BinlogSyncerConfig{
		ServerID:                c.cfg.ServerID,
		Flavor:                  c.cfg.Flavor,
		User:                    c.cfg.User,
		Password:                c.cfg.Password,
		Charset:                 c.cfg.Charset,
		HeartbeatPeriod:         c.cfg.HeartbeatPeriod,
		ReadTimeout:             c.cfg.ReadTimeout,
		UseDecimal:              c.cfg.UseDecimal,
		ParseTime:               c.cfg.ParseTime,
		SemiSyncEnabled:         c.cfg.SemiSyncEnabled,
		MaxReconnectAttempts:    c.cfg.MaxReconnectAttempts,
		DisableRetrySync:        c.cfg.DisableRetrySync,
		TimestampStringLocation: c.cfg.TimestampStringLocation,
		TLSConfig:               c.cfg.TLSConfig,
		Logger:                  c.cfg.Logger,
	}

	if strings.Contains(c.cfg.Addr, "/") {
		cfg.Host = c.cfg.Addr
	} else {
		seps := strings.Split(c.cfg.Addr, ":")
		if len(seps) != 2 {
			return errors.Errorf("invalid mysql addr format %s, must host:port", c.cfg.Addr)
		}

		port, err := strconv.ParseUint(seps[1], 10, 16)
		if err != nil {
			return errors.Trace(err)
		}

		cfg.Host = seps[0]
		cfg.Port = uint16(port)
	}

	c.syncer = replication.NewBinlogSyncer(cfg)

	return nil
}

// Execute a SQL
func (c *Canal) Execute(cmd string, args ...interface{}) (rr *mysql.Result, err error) {
	c.connLock.Lock()
	defer c.connLock.Unlock()
	argF := make([]func(*client.Conn), 0)
	if c.cfg.TLSConfig != nil {
		argF = append(argF, func(conn *client.Conn) {
			conn.SetTLSConfig(c.cfg.TLSConfig)
		})
	}
	retryNum := 3
	for i := 0; i < retryNum; i++ {
		if c.conn == nil {
			c.conn, err = client.Connect(c.cfg.Addr, c.cfg.User, c.cfg.Password, "", argF...)
			if err != nil {
				return nil, errors.Trace(err)
			}
		}

		rr, err = c.conn.Execute(cmd, args...)
		if err != nil && !mysql.ErrorEqual(err, mysql.ErrBadConn) {
			return
		} else if mysql.ErrorEqual(err, mysql.ErrBadConn) {
			c.conn.Close()
			c.conn = nil
			continue
		} else {
			return
		}
	}
	return
}

func (c *Canal) SyncedPosition() mysql.Position {
	return c.master.Position()
}

func (c *Canal) SyncedTimestamp() uint32 {
	return c.master.timestamp
}

func (c *Canal) SyncedGTIDSet() mysql.GTIDSet {
	return c.master.GTIDSet()
}
