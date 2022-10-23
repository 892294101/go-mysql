package canal

import (
	"crypto/tls"
	"io/ioutil"
	"math/rand"
	"time"

	"github.com/892294101/go-mysql/mysql"
	"github.com/BurntSushi/toml"
	"github.com/pingcap/errors"
	"github.com/sirupsen/logrus"
)

type Config struct {
	Addr     string `toml:"addr"`
	User     string `toml:"user"`
	Password string `toml:"password"`

	Charset         string        `toml:"charset"`
	ServerID        uint32        `toml:"server_id"`
	Flavor          string        `toml:"flavor"`
	HeartbeatPeriod time.Duration `toml:"heartbeat_period"`
	ReadTimeout     time.Duration `toml:"read_timeout"`

	// IncludeTableRegex or ExcludeTableRegex should contain database name
	// Only a table which matches IncludeTableRegex and dismatches ExcludeTableRegex will be processed
	// eg, IncludeTableRegex : [".*\\.canal"], ExcludeTableRegex : ["mysql\\..*"]
	//     this will include all database's 'canal' table, except database 'mysql'
	// Default IncludeTableRegex and ExcludeTableRegex are empty, this will include all tables
	IncludeTableRegex []string `toml:"include_table_regex"`
	ExcludeTableRegex []string `toml:"exclude_table_regex"`

	// discard row event without table meta
	DiscardNoMetaRowEvent bool `toml:"discard_no_meta_row_event"`

	UseDecimal bool `toml:"use_decimal"`
	ParseTime  bool `toml:"parse_time"`

	TimestampStringLocation *time.Location

	// SemiSyncEnabled enables semi-sync or not.
	SemiSyncEnabled bool `toml:"semi_sync_enabled"`

	// maximum number of attempts to re-establish a broken connection, zero or negative number means infinite retry.
	// this configuration will not work if DisableRetrySync is true
	MaxReconnectAttempts int `toml:"max_reconnect_attempts"`

	// whether disable re-sync for broken connection
	DisableRetrySync bool `toml:"disable_retry_sync"`

	// Set TLS config
	TLSConfig *tls.Config

	//Set Logger
	Logger *logrus.Logger
}

func NewConfigWithFile(name string) (*Config, error) {
	data, err := ioutil.ReadFile(name)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return NewConfig(string(data))
}

func NewConfig(data string) (*Config, error) {
	var c Config

	_, err := toml.Decode(data, &c)
	if err != nil {
		return nil, errors.Trace(err)
	}

	return &c, nil
}

func NewDefaultConfig() *Config {
	c := new(Config)

	c.Addr = "127.0.0.1:3306"
	c.User = "root"
	c.Password = ""

	c.Charset = mysql.DEFAULT_CHARSET
	c.ServerID = uint32(rand.New(rand.NewSource(time.Now().Unix())).Intn(1000)) + 1001

	c.Flavor = "mysql"
	return c
}
