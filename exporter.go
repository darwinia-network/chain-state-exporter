package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strconv"
	"time"

	ws "github.com/gorilla/websocket"
	"github.com/itering/subscan/util/ss58"
	"github.com/itering/substrate-api-rpc"
	"github.com/itering/substrate-api-rpc/util"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
)

type Exporter struct {
	endpoint           string
	registry           *prometheus.Registry
	metricDescriptions map[string]*prometheus.Desc
}

const (
	metadataSpecID  = 1
	ss58AddressType = 18
)

func NewExporter(endpoint string, customTypesFilePath string) (*Exporter, error) {
	conn, _, err := ws.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		return nil, err
	}

	defer conn.Close()

	if err := prepareMetadata(conn); err != nil {
		return nil, err
	}

	if customTypes, err := ioutil.ReadFile(customTypesFilePath); err == nil {
		substrate.RegCustomTypes(customTypes)
	} else {
		return nil, err
	}

	e := &Exporter{
		endpoint: endpoint,
		registry: prometheus.NewRegistry(),
	}

	e.metricDescriptions = map[string]*prometheus.Desc{}

	for k, desc := range map[string]struct {
		txt  string
		lbls []string
	}{
		"last_scrape_error":            {txt: "Whether the last scrape of metrics resulted in an error (1 for error, 0 for success)"},
		"last_scrape_duration_seconds": {txt: "Time duration of last scrape in seconds"},

		"active_era_index":      {txt: "From chain storage staking.activeEra"},
		"session_index":         {txt: "From chain storage session.currentIndex"},
		"validators_total":      {txt: "From chain storage session.validators"},
		"era_reward_points":     {txt: "From chain storage staking.erasRewardPoints", lbls: []string{"account_id", "address"}},
		"pending_headers_total": {txt: "From chain storage ethereumRelay.pendingRelayHeaderParcels"},
	} {
		e.metricDescriptions[k] = e.newMetricDesc(k, desc.txt, desc.lbls)
	}

	e.registry.MustRegister(e)

	return e, nil
}

func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	start := time.Now()

	var failed float64 = 0
	if err := e.dialDarwiniaNode(ch); err != nil {
		failed = 1
		logrus.Warnf("Scrape failed: %v", err)
	}

	e.registerConstMetricGauge(ch, "last_scrape_error", failed)
	e.registerConstMetricGauge(ch, "last_scrape_duration_seconds", time.Since(start).Seconds())
}

func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range e.metricDescriptions {
		ch <- desc
	}
}

func (e *Exporter) dialDarwiniaNode(ch chan<- prometheus.Metric) error {
	conn, _, err := ws.DefaultDialer.Dial(e.endpoint, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	var activeEra struct {
		Index uint32 `json:"index"`
	}
	if storage, err := readStorage(conn, "staking", "activeEra"); err != nil {
		return err
	} else if err = json.Unmarshal([]byte(storage), &activeEra); err != nil {
		return fmt.Errorf("storage staking.activeEra invalid: %w", err)
	} else {
		e.registerConstMetricCounter(ch, "active_era_index", float64(activeEra.Index))
	}

	var sessionIndex int
	if storage, err := readStorage(conn, "session", "currentIndex"); err != nil {
		return err
	} else if sessionIndex, err = strconv.Atoi(storage.ToString()); err != nil {
		return fmt.Errorf("storage session.currentIndex invaild: %w", err)
	} else {
		e.registerConstMetricCounter(ch, "session_index", float64(sessionIndex))
	}

	var validators []string
	if storage, err := readStorage(conn, "session", "validators"); err != nil {
		return err
	} else if err = json.Unmarshal([]byte(storage), &validators); err != nil {
		return fmt.Errorf("storage session.validators invalid: %w", err)
	} else {
		e.registerConstMetricGauge(ch, "validators_total", float64(len(validators)))
	}

	encodedActiveEra := make([]byte, 4)
	binary.LittleEndian.PutUint32(encodedActiveEra, activeEra.Index)

	var points EraRewardPoints
	if storage, err := readStorage(conn, "staking", "erasRewardPoints", util.BytesToHex(encodedActiveEra)); err != nil {
		return err
	} else if err = json.Unmarshal([]byte(storage), &points); err != nil {
		return fmt.Errorf("storage staking.erasRewardPoints invalid: %w", err)
	} else {
		for _, accountId := range validators {
			hasPoints := false
			address := ss58.Encode("0x"+accountId, ss58AddressType)
			for _, individual := range points.Individuals {
				if individual.AccountId == accountId {
					hasPoints = true
					e.registerConstMetricCounter(ch, "era_reward_points", float64(individual.RewardPoint), accountId, address)
				}
			}
			if !hasPoints {
				e.registerConstMetricCounter(ch, "era_reward_points", 0, accountId, address)
			}
		}
	}

	if storage, err := readStorage(conn, "ethereumRelay", "pendingRelayHeaderParcels"); err != nil {
		return err
	} else if storage == "null" {
		e.registerConstMetricGauge(ch, "pending_headers_total", 0)
	} else {
		var pendingHeaders []interface{}
		if err := json.Unmarshal([]byte(storage), &pendingHeaders); err != nil {
			return fmt.Errorf("storage ethereumRelay.pendingRelayHeaderParcels invalid: %w", err)
		}
		e.registerConstMetricGauge(ch, "pending_headers_total", float64(len(pendingHeaders)))
	}

	return nil
}

func (e *Exporter) registerConstMetricGauge(ch chan<- prometheus.Metric, metric string, val float64, labels ...string) {
	e.registerConstMetric(ch, metric, val, prometheus.GaugeValue, labels...)
}

func (e *Exporter) registerConstMetricCounter(ch chan<- prometheus.Metric, metric string, val float64, labels ...string) {
	e.registerConstMetric(ch, metric, val, prometheus.CounterValue, labels...)
}

func (e *Exporter) registerConstMetric(ch chan<- prometheus.Metric, metric string, val float64, valType prometheus.ValueType, labelValues ...string) {
	descr := e.metricDescriptions[metric]
	if descr == nil {
		descr = e.newMetricDesc(metric, metric+" metric", nil)
	}

	if m, err := prometheus.NewConstMetric(descr, valType, val, labelValues...); err == nil {
		ch <- m
	} else {
		logrus.Debugf("NewConstMetric() err: %s", err)
	}
}

func (e *Exporter) newMetricDesc(metricName string, docString string, labels []string) *prometheus.Desc {
	return prometheus.NewDesc(prometheus.BuildFQName("darwinia_state", "", metricName), docString, labels, nil)
}
