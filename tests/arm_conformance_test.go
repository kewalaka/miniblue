package tests

import (
	"testing"

	"github.com/moabukar/miniblue/internal/arm/armtest"
	"github.com/moabukar/miniblue/internal/server"
)

func TestARMConformance(t *testing.T) {
	srv := server.New()
	samples := append([]armtest.Sample{}, policySamples...)

	armtest.Coverage(t, srv.ARMRegistry(), samples)
	armtest.Run(t, srv.Handler(), srv.ARMRegistry(), samples)
}
