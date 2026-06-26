package otlp

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/pprof/profile"
	"github.com/grafana/alloy/internal/component/pyroscope"
	"github.com/prometheus/prometheus/model/labels"
	collectorprofilespb "go.opentelemetry.io/proto/otlp/collector/profiles/v1development"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	profilespb "go.opentelemetry.io/proto/otlp/profiles/v1development"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const defaultTimeout = 10 * time.Second

var (
	_ pyroscope.Appendable = (*otlpExporter)(nil)
	_ pyroscope.Appender   = (*otlpExporter)(nil)
)

type Arguments struct {
	Endpoint       string            `alloy:"endpoint,attr"`
	Insecure       bool              `alloy:"insecure,attr,optional"`
	Timeout        time.Duration     `alloy:"timeout,attr,optional"`
	ExternalLabels map[string]string `alloy:"external_labels,attr,optional"`
}

func (a *Arguments) SetToDefault() {
	a.Timeout = defaultTimeout
	a.Insecure = true
}

func (a Arguments) Validate() error {
	if a.Endpoint == "" {
		return errors.New("endpoint must not be empty")
	}
	return nil
}

type Exports struct {
	Receiver pyroscope.Appendable `alloy:"receiver,attr"`
}

type Component struct {
	logger        log.Logger
	onStateChange func(Exports)

	mu       sync.RWMutex
	args     Arguments
	exporter *otlpExporter
	conn     *grpc.ClientConn
}

func New(logger log.Logger, onStateChange func(Exports), args Arguments) (*Component, error) {
	c := &Component{
		logger:        logger,
		onStateChange: onStateChange,
	}
	if err := c.Update(args); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Component) Run(ctx context.Context) error {
	<-ctx.Done()
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.mu.Unlock()
	return ctx.Err()
}

func (c *Component) Update(args Arguments) error {
	if args.Timeout == 0 {
		args.Timeout = defaultTimeout
	}
	if err := args.Validate(); err != nil {
		return err
	}

	conn, err := dial(args)
	if err != nil {
		return err
	}
	exporter := &otlpExporter{
		logger:         c.logger,
		client:         collectorprofilespb.NewProfilesServiceClient(conn),
		timeout:        args.Timeout,
		externalLabels: args.ExternalLabels,
	}

	c.mu.Lock()
	oldConn := c.conn
	c.args = args
	c.conn = conn
	c.exporter = exporter
	c.mu.Unlock()

	if oldConn != nil {
		_ = oldConn.Close()
	}
	c.onStateChange(Exports{Receiver: exporter})
	return nil
}

func dial(args Arguments) (*grpc.ClientConn, error) {
	var transport credentials.TransportCredentials
	if args.Insecure {
		transport = insecure.NewCredentials()
	} else {
		transport = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return grpc.NewClient(args.Endpoint, grpc.WithTransportCredentials(transport))
}

type otlpExporter struct {
	logger         log.Logger
	client         collectorprofilespb.ProfilesServiceClient
	timeout        time.Duration
	externalLabels map[string]string
}

func (e *otlpExporter) Appender() pyroscope.Appender {
	return e
}

func (e *otlpExporter) Append(ctx context.Context, lbs labels.Labels, samples []*pyroscope.RawSample) error {
	var errs error
	for _, sample := range samples {
		if sample == nil || len(sample.RawProfile) == 0 {
			continue
		}
		profiles, err := e.rawSampleToProfiles(sample.RawProfile, lbs)
		if err != nil {
			errs = errors.Join(errs, err)
			continue
		}
		if err := e.exportProfiles(ctx, profiles); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func (e *otlpExporter) AppendIngest(ctx context.Context, incoming *pyroscope.IncomingProfile) error {
	if incoming == nil || len(incoming.RawBody) == 0 {
		return nil
	}
	profiles, err := e.rawSampleToProfiles(incoming.RawBody, incoming.Labels)
	if err != nil {
		return err
	}
	return e.exportProfiles(ctx, profiles)
}

func (e *otlpExporter) rawSampleToProfiles(raw []byte, lbs labels.Labels) (*collectorprofilespb.ExportProfilesServiceRequest, error) {
	pprofProfile, err := profile.ParseData(raw)
	if err != nil {
		return nil, fmt.Errorf("parse pprof profile: %w", err)
	}
	req, err := convertPprofToOTLPRequest(pprofProfile)
	if err != nil {
		return nil, fmt.Errorf("convert pprof to otlp profiles: %w", err)
	}
	addResourceAttributes(req, lbs, e.externalLabels)
	return req, nil
}

func (e *otlpExporter) exportProfiles(ctx context.Context, req *collectorprofilespb.ExportProfilesServiceRequest) error {
	exportCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	_, err := e.client.Export(exportCtx, req)
	if err != nil {
		return fmt.Errorf("export otlp profiles: %w", err)
	}
	level.Debug(e.logger).Log("msg", "exported otlp profiles", "resource_profiles", len(req.ResourceProfiles))
	return nil
}

func addResourceAttributes(req *collectorprofilespb.ExportProfilesServiceRequest, lbs labels.Labels, externalLabels map[string]string) {
	if len(req.ResourceProfiles) == 0 {
		req.ResourceProfiles = append(req.ResourceProfiles, &profilespb.ResourceProfiles{})
	}
	for _, rp := range req.ResourceProfiles {
		if rp.Resource == nil {
			rp.Resource = &resourcepb.Resource{}
		}
		for k, v := range externalLabels {
			rp.Resource.Attributes = append(rp.Resource.Attributes, stringKeyValue(k, v))
		}
		lbs.Range(func(l labels.Label) {
			if l.Name == "" {
				return
			}
			rp.Resource.Attributes = append(rp.Resource.Attributes, stringKeyValue(l.Name, l.Value))
		})
	}
}

func stringKeyValue(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{
		Key:   key,
		Value: stringAnyValue(value),
	}
}
