package replication

import (
	"context"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"math/rand"
	"net"
	"time"
)

func NewRawDataDecode(log *logrus.Logger) (*BinlogSyncer, error) {
	local, _ := time.LoadLocation("Asia/Shanghai")
	cfg := BinlogSyncerConfig{
		ServerID:                uint32(rand.New(rand.NewSource(time.Now().UnixNano())).Int31n(11111)),
		Flavor:                  "mysql",
		Logger:                  log,
		UseDecimal:              true,
		ParseTime:               true,
		TimestampStringLocation: local,
	}

	if cfg.TimestampStringLocation == nil {
		return nil, errors.Errorf("%s\n", "time zone cannot be empty")
	}

	if cfg.Logger == nil {
		return nil, errors.Errorf("%s\n", "Log module cannot be empty")
	}
	if cfg.ServerID == 0 {
		return nil, errors.Errorf("can't use 0 as the server ID")
	}
	if cfg.Dialer == nil {
		dialer := &net.Dialer{}
		cfg.Dialer = dialer.DialContext
	}

	b := new(BinlogSyncer)

	b.cfg = cfg
	b.parser = NewBinlogParser()
	b.parser.SetFlavor(cfg.Flavor)
	b.parser.SetRawMode(b.cfg.RawModeEnabled)
	b.parser.SetParseTime(b.cfg.ParseTime)
	b.parser.SetTimestampStringLocation(b.cfg.TimestampStringLocation)
	b.parser.SetUseDecimal(b.cfg.UseDecimal)
	b.parser.SetVerifyChecksum(b.cfg.VerifyChecksum)
	b.ctx, b.cancel = context.WithCancel(context.Background())
	return b, nil
}

func (b *BinlogSyncer) Decode(data []byte) (*BinlogEvent, error) {
	//skip OK byte, 0x00
	data = data[1:]

	if b.cfg.SemiSyncEnabled && (data[0] == SemiSyncIndicator) {
		data = data[2:]
	}

	e, err := b.parser.Parse(data)
	if err != nil {
		return nil, err
	}
	return e, nil

}
