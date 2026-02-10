package relayer

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type PromMetrics struct {
	WalletBalance         *prometheus.GaugeVec
	LatestHeight          *prometheus.GaugeVec
	BroadcastErrors       *prometheus.CounterVec
	FastTransferAllowance *prometheus.GaugeVec
	AttestationTotal      *prometheus.CounterVec
	AttestationPending    *prometheus.GaugeVec
}

func InitPromMetrics(address string, port int16) *PromMetrics {
	reg := prometheus.NewRegistry()

	// labels
	var (
		walletLabels         = []string{"chain", "address", "denom"}
		heightLabels         = []string{"chain", "domain"}
		broadcastErrorLabels = []string{"chain", "domain"}
		allowanceLabels      = []string{"chain", "domain", "token"}
		attestationLabels    = []string{"src_chain", "dest_chain", "status", "source_domain", "dest_domain"}
		pendingLabels        = []string{"src_chain", "dest_chain", "source_domain", "dest_domain"}
	)

	m := &PromMetrics{
		WalletBalance: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cctp_relayer_wallet_balance",
			Help: "The current balance for a wallet",
		}, walletLabels),
		LatestHeight: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cctp_relayer_chain_latest_height",
			Help: "The current height of the chain",
		}, heightLabels),
		BroadcastErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cctp_relayer_broadcast_errors_total",
			Help: "The total number of failed broadcasts. Note: this is AFTER is retires `broadcast-retries` number of times (config setting).",
		}, broadcastErrorLabels),
		FastTransferAllowance: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cctp_relayer_fast_transfer_allowance",
			Help: "Current Fast Transfer allowance for a domain (v2 only)",
		}, allowanceLabels),
		AttestationTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cctp_relayer_attestation_total",
			Help: "Attestation state transitions: observed, pending, complete, failed, filtered, minted",
		}, attestationLabels),
		AttestationPending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cctp_relayer_attestation_pending",
			Help: "Number of attestations currently pending",
		}, pendingLabels),
	}

	reg.MustRegister(m.WalletBalance)
	reg.MustRegister(m.LatestHeight)
	reg.MustRegister(m.BroadcastErrors)
	reg.MustRegister(m.FastTransferAllowance)
	reg.MustRegister(m.AttestationTotal)
	reg.MustRegister(m.AttestationPending)

	// Expose /metrics HTTP endpoint
	go func() {
		http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
		server := &http.Server{
			Addr:        fmt.Sprintf("%s:%d", address, port),
			ReadTimeout: 3 * time.Second,
		}
		log.Fatal(server.ListenAndServe())
	}()

	return m
}

func (m *PromMetrics) SetWalletBalance(chain, address, denom string, balance float64) {
	m.WalletBalance.WithLabelValues(chain, address, denom).Set(balance)
}

func (m *PromMetrics) SetLatestHeight(chain, domain string, height int64) {
	m.LatestHeight.WithLabelValues(chain, domain).Set(float64(height))
}

func (m *PromMetrics) IncBroadcastErrors(chain, domain string) {
	m.BroadcastErrors.WithLabelValues(chain, domain).Inc()
}

func (m *PromMetrics) SetFastTransferAllowance(chain, domain, token string, allowance float64) {
	m.FastTransferAllowance.WithLabelValues(chain, domain, token).Set(allowance)
}

func (m *PromMetrics) IncAttestation(srcChain, destChain, status, srcDomain, destDomain string) {
	m.AttestationTotal.WithLabelValues(srcChain, destChain, status, srcDomain, destDomain).Inc()
}

func (m *PromMetrics) IncPending(srcChain, destChain, srcDomain, destDomain string) {
	m.AttestationPending.WithLabelValues(srcChain, destChain, srcDomain, destDomain).Inc()
}

func (m *PromMetrics) DecPending(srcChain, destChain, srcDomain, destDomain string) {
	m.AttestationPending.WithLabelValues(srcChain, destChain, srcDomain, destDomain).Dec()
}
