package otlp

import (
	"github.com/grafana/alloy/internal/component"
	"github.com/grafana/alloy/internal/component/pyroscope/util/glue"
	"github.com/grafana/alloy/internal/featuregate"
)

func init() {
	component.Register(component.Registration{
		Name:      "pyroscope.exporter.otlp",
		Stability: featuregate.StabilityExperimental,
		Args:      Arguments{},
		Exports:   Exports{},
		Build: func(o component.Options, c component.Arguments) (component.Component, error) {
			args := c.(Arguments)
			gc, err := New(o.Logger, func(exports Exports) {
				o.OnStateChange(exports)
			}, args)
			if err != nil {
				return nil, err
			}
			return &glue.GenericComponentGlue[Arguments]{Impl: gc}, nil
		},
	})
}
