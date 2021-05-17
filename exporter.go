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

		"active_era_index":  {txt: "From chain storage staking.activeEra"},
		"session_index":     {txt: "From chain storage session.currentIndex"},
		"validators_total":  {txt: "From chain storage session.validators"},
		"era_reward_points": {txt: "From chain storage staking.erasRewardPoints", lbls: []string{"account_id", "address"}},

		"best_confirmed_ethereum_block_number": {txt: "From chain storage ethereumRelay.bestConfirmedBlockNumber"},
		"pending_headers_total":                {txt: "From chain storage ethereumRelay.pendingRelayHeaderParcels"},
		"pending_header_ethereum_block_number": {txt: "From chain storage ethereumRelay.pendingRelayHeaderParcels", lbls: []string{"block_number"}},
		"mmr_roots_to_sign_total":              {txt: "From chain storage ethereumRelayAuthorities.mMRRootsToSignKeys"},
		"authorities_to_sign":                  {txt: "From chain storage ethereumRelayAuthorities.authoritiesToSign"},
		"authorities_to_sign_votes":            {txt: "From chain storage ethereumRelayAuthorities.authoritiesToSign"},
		"authorities_to_sign_deadline":         {txt: "From chain storage ethereumRelayAuthorities.nextAuthorities"},
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

	//
	// Substrate common storage
	//
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

	//
	// Darwinia specific storage
	//
	if storage, err := readStorage(conn, "ethereumRelay", "bestConfirmedBlockNumber"); err != nil {
		return err
	} else if blockNumber, err := strconv.Atoi(storage.ToString()); err != nil {
		return fmt.Errorf("storage ethereumRelay.bestConfirmedBlockNumber invaild: %w", err)
	} else {
		e.registerConstMetricCounter(ch, "best_confirmed_ethereum_block_number", float64(blockNumber))
	}

	var pendingHeaders []struct {
		BlockNumber               uint32 `json:"col1"`
		EthereumRelayHeaderParcel struct {
			Header struct {
				Number uint32 `json:"number"`
			} `json:"header"`
		} `json:"col2"`
	}
	// storage = `[{"col1":3311901,"col2":{"header":{"author":"0x5a0b54d5dc17e0aadc383d2db43b0a0d3e029c4c","difficulty":"0x3b0291df1cb21b00000000000000000000000000000000000000000000000000","extra_data":"eth-pro-hzo-t006","gas_limit":"0x04a7e40000000000000000000000000000000000000000000000000000000000","gas_used":"0xdd2de40000000000000000000000000000000000000000000000000000000000","hash":"0xa92fd48e8334b54637489d311f4ca6f9bd4ec2928cb72153f58e78ecc8d4ae23","log_bloom":"0x51e6122f490e11b69ec13448e60b1e3ea104670084125222e1d96feab1008c736414277de24a3285e58210848480416b06cc509b2801e8bd0a50a0aa3f38997f604990108a043b1df99b451b03002069040e248d927246181b42164ddcf84541d1d6180f3320fc7d02053c0405593888255000611242c6ea616bdc9793da67443920808337c42260a9cc59080f34182e50c810c343c0d12e5024cd65225a08db469001452f9a20f141854ea1c12e0151d864288828631b99090b96e001c07165e11d064b116a04c3885705a0085211c54a0a265895c8b61c086d30af044af823b1f8a520a47c010db8809460c1123000f7e446e05625e5d03700ca84453afac0","number":12424914,"parent_hash":"0x0951b58b7c5ee2ff8bfc434b6c2ef91823275b544abd8b41440edc8fb3b3bab3","receipts_root":"0x991ddc463cfae902cdc851b3a02148ad041ba8197b3a167eb7d3f5c54f4b11b6","seal":["a08a4fe7cfed681690e8e2cf4a844afe866deeba18d09f470bf84031b13fdebb2f","881e982d6cd0d6413d"],"state_root":"0x2996266cdb262e8d388e5356ee2a2727c6393abb33e2208036595cd152d1771d","timestamp":1620893647,"transactions_root":"0xc0d88d5eb1281ef918439e438ec85e2a3b5b5407ccb1450f9a1bf1defba55a3b","uncles_hash":"0x1dcc4de8dec75d7aab85b567b6ccd41ad312451b948a7413f0a142fd40d49347"},"parent_mmr_root":"0xe2d2836e5aab480bd2fbe788efff55518e764094b250e5bcd8ba540499a5d8c4"},"col3":{"ayes":["b4f7f03bebc56ebe96bc52ea5ed3159d45a0ce3a8d7f082983c33ef133274747"],"nays":null}}]`
	if storage, err := readStorage(conn, "ethereumRelay", "pendingRelayHeaderParcels"); err != nil {
		return err
	} else if storage == "null" {
		e.registerConstMetricGauge(ch, "pending_headers_total", 0)
	} else if err := json.Unmarshal([]byte(storage), &pendingHeaders); err != nil {
		return fmt.Errorf("storage ethereumRelay.pendingRelayHeaderParcels invalid: %w", err)
	} else {
		e.registerConstMetricGauge(ch, "pending_headers_total", float64(len(pendingHeaders)))
		for _, header := range pendingHeaders {
			darwiniaBlockNumber := fmt.Sprint(header.BlockNumber)
			ethereumBlockNumber := header.EthereumRelayHeaderParcel.Header.Number
			e.registerConstMetricGauge(ch, "pending_header_ethereum_block_number", float64(float64(ethereumBlockNumber)), darwiniaBlockNumber)
		}
	}

	var mmrRootsToSignKeys []uint64
	if storage, err := readStorage(conn, "ethereumRelayAuthorities", "mMRRootsToSignKeys"); err != nil {
		return err
	} else if err = json.Unmarshal([]byte(storage), &mmrRootsToSignKeys); err != nil {
		return fmt.Errorf("storage ethereumRelayAuthorities.mMRRootsToSignKeys invalid: %w", err)
	} else {
		e.registerConstMetricGauge(ch, "mmr_roots_to_sign_total", float64(len(mmrRootsToSignKeys)))
	}

	var authoritiesToSign struct {
		RelayAuthorityMessage string        `json:"col1"`
		Votes                 []interface{} `json:"col2"`
	}
	if storage, err := readStorage(conn, "ethereumRelayAuthorities", "authoritiesToSign"); err != nil {
		return err
	} else if storage == "null" {
		e.registerConstMetricGauge(ch, "authorities_to_sign", 0)
		e.registerConstMetricGauge(ch, "authorities_to_sign_votes", 0)
	} else {
		e.registerConstMetricGauge(ch, "authorities_to_sign", 1)
		if err = json.Unmarshal([]byte(storage), &authoritiesToSign); err != nil {
			return fmt.Errorf("storage ethereumRelayAuthorities.authoritiesToSign invalid: %w", err)
		} else {
			e.registerConstMetricGauge(ch, "authorities_to_sign_votes", float64(len(authoritiesToSign.Votes)))
		}
	}

	var scheduledAuthoritiesChange struct {
		NextAuthorities []interface{} `json:"next_authorities"`
		Deadline        uint64        `json:"deadline"`
	}
	if storage, err := readStorage(conn, "ethereumRelayAuthorities", "nextAuthorities"); err != nil {
		return err
	} else if storage == "null" {
		e.registerConstMetricGauge(ch, "authorities_to_sign_deadline", 0)
	} else if err = json.Unmarshal([]byte(storage), &scheduledAuthoritiesChange); err != nil {
		return fmt.Errorf("storage ethereumRelayAuthorities.nextAuthorities invalid: %w", err)
	} else {
		e.registerConstMetricGauge(ch, "authorities_to_sign_deadline", float64(scheduledAuthoritiesChange.Deadline))
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
