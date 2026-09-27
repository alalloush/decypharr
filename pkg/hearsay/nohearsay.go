//go:build nohearsay

// Package hearsay is compiled out: this build carries the nohearsay tag, so
// the engine, its P2P transport and their dependencies are not linked. Every
// method is a no-op, as on a disabled service.
package hearsay

import (
	"context"

	"github.com/rs/zerolog"

	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/usenet/parser"
)

type Service struct{}

// New logs that hearsay is compiled out, unless the config disables it
// anyway, and returns a nil service.
func New(cfg *config.Config, log zerolog.Logger) (*Service, error) {
	if !cfg.Hearsay.Disabled && cfg.Hearsay.Participates() {
		log.Warn().Msg("hearsay.participate is set, but this build excludes hearsay (nohearsay tag)")
	}
	return nil, nil
}

func (s *Service) Start(context.Context) error { return nil }

func (s *Service) Close() {}

// Status reports participation state for the API and UI.
type Status struct {
	Enabled bool `json:"enabled"`
}

func (s *Service) Status() Status { return Status{} }

func (s *Service) ObserveTorrent(vendor, infohash string, present bool) {}

func (s *Service) ReportAdd(vendor, infohash string, instant bool) {}

type AddDecision struct{}

func (d AddDecision) Reject() bool { return false }

func (s *Service) EvaluateAdd(vendor, infohash string) AddDecision { return AddDecision{} }

func (s *Service) RecordAdd(decision AddDecision, instant bool) {}

func (s *Service) DiscardAdd(decision AddDecision) {}

func (s *Service) ReportNZB(subject string, complete bool) {}

func (s *Service) NZBClaimedIncomplete(subject string) bool { return false }

func NZBSubjectFromGroups(groups map[string]*parser.FileGroup) string { return "" }
