// @feature cert-volcano-deployer @api-functional
//
// Volcano cert-library SDK stub for the volcano-cert-replacement journey
// tests. Satisfies the unexported deployer adapter interface
// ( volcanoCertLibraryAPI ) by method-set compatibility — the same pattern the
// multicloudtest harness uses for huawei/aws/azure — records every call, and
// exposes in-cloud state so journeys can drive the two-phase deploy, bind
// failure compensation, rollback precheck ( GetCert ) and orphan cleanup
// hermetically.
//
// Library semantics mirror the real cloud APIs:
//   - Get/Describe misses report empty results ( Exists=false );
//   - Delete of an already-deleted ID returns a cloud-side not-found error
//     ( the deployer normalizes it to idempotent success — double-cleanup
//     journeys exercise exactly this path );
//   - WAF/ALB libraries provide no certificate fingerprint ( GetCert
//     fingerprint resolution falls back to the mapping reverse-lookup, which
//     the rollback journey depends on ); CDN provides the native SHA-256.

package volcano_cert_replacement

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	volcanosdkalb "github.com/volcengine/volcengine-go-sdk/service/alb"
	volcanosdkcdn "github.com/volcengine/volcengine-go-sdk/service/cdn"
	volcanosdkcsv "github.com/volcengine/volcengine-go-sdk/service/certificateservice"
	volcanosdkclb "github.com/volcengine/volcengine-go-sdk/service/clb"
	volcanosdkwaf "github.com/volcengine/volcengine-go-sdk/service/waf"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
)

// volcanoBindCall is one recorded bind invocation ( raw cloud cert ID form,
// as the per-library bind API received it ).
type volcanoBindCall struct {
	Product     string
	ResourceID  string
	CloudCertID string
}

// csvInstance is one certificateservice instance in the csv library.
type csvInstance struct {
	status  string
	revoked bool
	chain   []string // PEM chain, leaf first
}

// cdnCert is one CDN library certificate: native SHA-256 fingerprint channel
// plus the configured acceleration domains ( reference enumeration ).
type cdnCert struct {
	sha256     string
	expireUnix int64
	domains    []string
}

// StubVolcanoCertLibrary stubs the volcano four-library SDK surface.
type StubVolcanoCertLibrary struct {
	mu sync.Mutex

	// Uploads: per-library counters + recorded "{product}:{rawID}" artifacts.
	csvUploads   int
	cdnUploads   int
	wafUploads   int
	albUploads   int
	uploadInputs []string

	// Binds: recorded calls + persistent per-product failure injection.
	binds    []volcanoBindCall
	bindErrs map[string]error

	// Deletes: recorded "{product}:{rawID}" + stateful idempotency ( a second
	// delete of the same ID answers the cloud-side not-found error ) and
	// injected persistent ( non-not-found ) errors.
	deletes    []string
	deleted    map[string]bool
	deleteErrs map[string]error

	// In-cloud library state.
	csvInstances map[string]*csvInstance
	cdnCerts     map[string]*cdnCert
	wafCerts     []*volcanosdkwaf.DataForListWafServiceCertificateOutput
	wafDomains   map[string]*volcanosdkwaf.DataForListDomainOutput
	albCerts     map[string]*volcanosdkalb.CertificateForDescribeCertificatesOutput
	albListeners []*volcanosdkalb.ListenerForDescribeListenersOutput
	nlbListeners []*volcanosdkclb.ListenerForDescribeNLBListenersOutput
}

// NewStubVolcanoCertLibrary creates an empty volcano stub.
func NewStubVolcanoCertLibrary() *StubVolcanoCertLibrary {
	return &StubVolcanoCertLibrary{
		bindErrs:     map[string]error{},
		deleted:      map[string]bool{},
		deleteErrs:   map[string]error{},
		csvInstances: map[string]*csvInstance{},
		cdnCerts:     map[string]*cdnCert{},
		wafDomains:   map[string]*volcanosdkwaf.DataForListDomainOutput{},
		albCerts:     map[string]*volcanosdkalb.CertificateForDescribeCertificatesOutput{},
	}
}

// ---------------------------------------------------------------------
// Configuration ( journey fixture setters )
// ---------------------------------------------------------------------

// SetCSVInstance registers a csv library instance ( GetCert chain-parse path ).
func (s *StubVolcanoCertLibrary) SetCSVInstance(id string, chainPEM []string, status string, revoked bool) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.csvInstances[id] = &csvInstance{status: status, revoked: revoked, chain: chainPEM}
	return s
}

// SetCDNCert registers a CDN library certificate with its native SHA-256
// fingerprint and configured acceleration domains.
func (s *StubVolcanoCertLibrary) SetCDNCert(certID, sha256 string, expireUnix int64, domains ...string) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cdnCerts[certID] = &cdnCert{sha256: sha256, expireUnix: expireUnix, domains: domains}
	return s
}

// SetWAFServiceCert registers a WAF service certificate ( no fingerprint
// channel — mirrors the real WAF library ).
func (s *StubVolcanoCertLibrary) SetWAFServiceCert(id int32, expireTime string) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wafCerts = append(s.wafCerts, &volcanosdkwaf.DataForListWafServiceCertificateOutput{
		Id:         volcengine.Int32(id),
		ExpireTime: volcengine.String(expireTime),
	})
	return s
}

// SetWAFDomain registers a WAF protected domain ( bind locate + reference
// enumeration ); AccessMode is read back verbatim by UpdateDomain.
func (s *StubVolcanoCertLibrary) SetWAFDomain(domain string, certID, accessMode int32) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wafDomains[domain] = &volcanosdkwaf.DataForListDomainOutput{
		Domain:        volcengine.String(domain),
		CertificateID: volcengine.Int32(certID),
		AccessMode:    volcengine.Int32(accessMode),
	}
	return s
}

// SetALBCert registers an ALB listener-library certificate ( shared by the
// alb/nlb prefixes; no fingerprint channel ).
func (s *StubVolcanoCertLibrary) SetALBCert(certID, expiredAt string) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.albCerts[certID] = &volcanosdkalb.CertificateForDescribeCertificatesOutput{
		CertificateId: volcengine.String(certID),
		ExpiredAt:     volcengine.String(expiredAt),
	}
	return s
}

// SetALBListener registers an ALB listener ( bind locate via ListenerIds
// filter + reference enumeration ).
func (s *StubVolcanoCertLibrary) SetALBListener(listenerID, lbID, protocol, certID string) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.albListeners = append(s.albListeners, &volcanosdkalb.ListenerForDescribeListenersOutput{
		ListenerId:     volcengine.String(listenerID),
		LoadBalancerId: volcengine.String(lbID),
		Protocol:       volcengine.String(protocol),
		CertificateId:  volcengine.String(certID),
	})
	return s
}

// SetNLBListener registers an NLB listener ( clb service NLB API ).
func (s *StubVolcanoCertLibrary) SetNLBListener(listenerID, lbID, certID string) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nlbListeners = append(s.nlbListeners, &volcanosdkclb.ListenerForDescribeNLBListenersOutput{
		ListenerId:     volcengine.String(listenerID),
		LoadBalancerId: volcengine.String(lbID),
		CertificateId:  volcengine.String(certID),
	})
	return s
}

// SetBindErr injects a persistent bind failure for one product ( non
// rate-limit error: the bounded retry does not back off, the item fails
// immediately ).
func (s *StubVolcanoCertLibrary) SetBindErr(product string, err error) *StubVolcanoCertLibrary {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindErrs[product] = err
	return s
}

// ---------------------------------------------------------------------
// Observers
// ---------------------------------------------------------------------

// BindCalls returns every recorded bind invocation in call order.
func (s *StubVolcanoCertLibrary) BindCalls() []volcanoBindCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]volcanoBindCall(nil), s.binds...)
}

// BindsByProduct returns the recorded bind invocations of one product.
func (s *StubVolcanoCertLibrary) BindsByProduct(product string) []volcanoBindCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []volcanoBindCall
	for _, b := range s.binds {
		if b.Product == product {
			out = append(out, b)
		}
	}
	return out
}

// UploadCounts returns per-library upload totals ( csv, cdn, waf, alb ).
func (s *StubVolcanoCertLibrary) UploadCounts() (csvN, cdnN, wafN, albN int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.csvUploads, s.cdnUploads, s.wafUploads, s.albUploads
}

// UploadInputs returns the recorded "{product}:{rawID}" upload artifacts.
func (s *StubVolcanoCertLibrary) UploadInputs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploadInputs...)
}

// Deletes returns every recorded delete invocation ("{product}:{rawID}").
func (s *StubVolcanoCertLibrary) Deletes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.deletes...)
}

// DeleteRecordedFor reports whether any recorded delete targeted rawID ( any
// library prefix — alb/nlb share one delete API ).
func (s *StubVolcanoCertLibrary) DeleteRecordedFor(rawID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range s.deletes {
		if _, raw, ok := strings.Cut(key, ":"); ok && raw == rawID {
			return true
		}
	}
	return false
}

// recordBind records one bind and applies the per-product failure injection.
func (s *StubVolcanoCertLibrary) recordBind(product, resourceID, certID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.binds = append(s.binds, volcanoBindCall{Product: product, ResourceID: resourceID, CloudCertID: certID})
	return s.bindErrs[product]
}

// recordDelete records one delete and answers the stateful idempotency
// semantics: first delete succeeds, a repeated delete of the same raw ID
// returns the cloud-side not-found error ( normalized to success by the
// deployer ), an injected non-not-found error always wins.
func (s *StubVolcanoCertLibrary) recordDelete(product, rawID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := product + ":" + rawID
	s.deletes = append(s.deletes, key)
	if err := s.deleteErrs[key]; err != nil {
		return err
	}
	if s.deleted[rawID] {
		return errors.New("Invalid" + product + ".NotFound: the " + product + " cert " + rawID + " not found")
	}
	s.deleted[rawID] = true
	return nil
}

// recordUpload bumps the per-library counter and records the artifact.
func (s *StubVolcanoCertLibrary) recordUpload(product, rawID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch product {
	case "csv":
		s.csvUploads++
	case "cdn":
		s.cdnUploads++
	case "waf":
		s.wafUploads++
	case "alb":
		s.albUploads++
	}
	s.uploadInputs = append(s.uploadInputs, product+":"+rawID)
}

// ---------------------------------------------------------------------
// certificateservice ( csv ) unified library
// ---------------------------------------------------------------------

func (s *StubVolcanoCertLibrary) ImportCertificateWithContext(_ context.Context, input *volcanosdkcsv.ImportCertificateInput, _ ...request.Option) (*volcanosdkcsv.ImportCertificateOutput, error) {
	s.mu.Lock()
	s.csvUploads++
	id := "inst-csv-" + strconv.Itoa(s.csvUploads)
	s.uploadInputs = append(s.uploadInputs, "csv:"+id)
	s.mu.Unlock()
	return &volcanosdkcsv.ImportCertificateOutput{InstanceId: volcengine.String(id)}, nil
}

func (s *StubVolcanoCertLibrary) CertificateGetInstanceWithContext(_ context.Context, input *volcanosdkcsv.CertificateGetInstanceInput, _ ...request.Option) (*volcanosdkcsv.CertificateGetInstanceOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	inst, ok := s.csvInstances[*input.InstanceId]
	if !ok {
		return nil, errors.New("InvalidInstanceId.NotFound: instance " + *input.InstanceId + " not found")
	}
	out := &volcanosdkcsv.CertificateGetInstanceOutput{
		Status:               volcengine.String(inst.status),
		IsCertificateRevoked: volcengine.Bool(inst.revoked),
	}
	detail := &volcanosdkcsv.CertificateDetailForCertificateGetInstanceOutput{}
	for _, pemStr := range inst.chain {
		chainPEM := pemStr
		detail.Chain = append(detail.Chain, &chainPEM)
	}
	out.CertificateDetail = detail
	return out, nil
}

func (s *StubVolcanoCertLibrary) CertificateDeleteInstanceWithContext(_ context.Context, input *volcanosdkcsv.CertificateDeleteInstanceInput, _ ...request.Option) (*volcanosdkcsv.CertificateDeleteInstanceOutput, error) {
	if err := s.recordDelete("csv", *input.InstanceId); err != nil {
		return nil, err
	}
	return &volcanosdkcsv.CertificateDeleteInstanceOutput{}, nil
}

// ---------------------------------------------------------------------
// CDN library
// ---------------------------------------------------------------------

func (s *StubVolcanoCertLibrary) AddCertificateWithContext(_ context.Context, _ *volcanosdkcdn.AddCertificateInput, _ ...request.Option) (*volcanosdkcdn.AddCertificateOutput, error) {
	s.mu.Lock()
	s.cdnUploads++
	id := "cert-cdn-" + strconv.Itoa(s.cdnUploads)
	s.uploadInputs = append(s.uploadInputs, "cdn:"+id)
	s.mu.Unlock()
	return &volcanosdkcdn.AddCertificateOutput{CertId: volcengine.String(id)}, nil
}

func (s *StubVolcanoCertLibrary) ListCertInfoWithContext(_ context.Context, input *volcanosdkcdn.ListCertInfoInput, _ ...request.Option) (*volcanosdkcdn.ListCertInfoOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cert, ok := s.cdnCerts[*input.CertId]
	if !ok {
		return &volcanosdkcdn.ListCertInfoOutput{}, nil
	}
	item := &volcanosdkcdn.CertInfoForListCertInfoOutput{
		CertId:     volcengine.String(*input.CertId),
		ExpireTime: volcengine.Int64(cert.expireUnix),
	}
	if cert.sha256 != "" {
		item.CertFingerprint = &volcanosdkcdn.CertFingerprintForListCertInfoOutput{Sha256: volcengine.String(cert.sha256)}
	}
	return &volcanosdkcdn.ListCertInfoOutput{CertInfo: []*volcanosdkcdn.CertInfoForListCertInfoOutput{item}}, nil
}

func (s *StubVolcanoCertLibrary) DeleteCdnCertificateWithContext(_ context.Context, input *volcanosdkcdn.DeleteCdnCertificateInput, _ ...request.Option) (*volcanosdkcdn.DeleteCdnCertificateOutput, error) {
	if err := s.recordDelete("cdn", *input.CertId); err != nil {
		return nil, err
	}
	return &volcanosdkcdn.DeleteCdnCertificateOutput{}, nil
}

func (s *StubVolcanoCertLibrary) BatchDeployCertWithContext(_ context.Context, input *volcanosdkcdn.BatchDeployCertInput, _ ...request.Option) (*volcanosdkcdn.BatchDeployCertOutput, error) {
	if err := s.recordBind("cdn", volcengine.StringValue(input.Domain), volcengine.StringValue(input.CertId)); err != nil {
		return nil, err
	}
	return &volcanosdkcdn.BatchDeployCertOutput{
		DeployResult: []*volcanosdkcdn.DeployResultForBatchDeployCertOutput{{
			Domain: input.Domain,
			Status: volcengine.String("success"),
		}},
	}, nil
}

func (s *StubVolcanoCertLibrary) ListCdnCertInfoWithContext(_ context.Context, input *volcanosdkcdn.ListCdnCertInfoInput, _ ...request.Option) (*volcanosdkcdn.ListCdnCertInfoOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []*volcanosdkcdn.CertInfoForListCdnCertInfoOutput
	for certID, cert := range s.cdnCerts {
		entry := &volcanosdkcdn.CertInfoForListCdnCertInfoOutput{CertId: volcengine.String(certID)}
		for _, dom := range cert.domains {
			entry.ConfiguredDomainDetail = append(entry.ConfiguredDomainDetail,
				&volcanosdkcdn.ConfiguredDomainDetailForListCdnCertInfoOutput{Domain: volcengine.String(dom)})
		}
		items = append(items, entry)
	}
	// Single page ( the journey world never exceeds the default page size ).
	if input.PageNum != nil && *input.PageNum > 1 {
		return &volcanosdkcdn.ListCdnCertInfoOutput{}, nil
	}
	return &volcanosdkcdn.ListCdnCertInfoOutput{CertInfo: items}, nil
}

// ---------------------------------------------------------------------
// WAF service library + protected domains
// ---------------------------------------------------------------------

func (s *StubVolcanoCertLibrary) UploadWafServiceCertificateWithContext(_ context.Context, _ *volcanosdkwaf.UploadWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.UploadWafServiceCertificateOutput, error) {
	s.mu.Lock()
	s.wafUploads++
	id := int32(30 + s.wafUploads)
	s.uploadInputs = append(s.uploadInputs, "waf:"+strconv.FormatInt(int64(id), 10))
	s.mu.Unlock()
	return &volcanosdkwaf.UploadWafServiceCertificateOutput{Id: volcengine.Int32(id)}, nil
}

func (s *StubVolcanoCertLibrary) ListWafServiceCertificateWithContext(_ context.Context, _ *volcanosdkwaf.ListWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.ListWafServiceCertificateOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &volcanosdkwaf.ListWafServiceCertificateOutput{Data: s.wafCerts}, nil
}

func (s *StubVolcanoCertLibrary) DeleteWafServiceCertificateWithContext(_ context.Context, input *volcanosdkwaf.DeleteWafServiceCertificateInput, _ ...request.Option) (*volcanosdkwaf.DeleteWafServiceCertificateOutput, error) {
	if err := s.recordDelete("waf", *input.Id); err != nil {
		return nil, err
	}
	return &volcanosdkwaf.DeleteWafServiceCertificateOutput{}, nil
}

func (s *StubVolcanoCertLibrary) ListDomainWithContext(_ context.Context, _ *volcanosdkwaf.ListDomainInput, _ ...request.Option) (*volcanosdkwaf.ListDomainOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []*volcanosdkwaf.DataForListDomainOutput
	for _, item := range s.wafDomains {
		items = append(items, item)
	}
	return &volcanosdkwaf.ListDomainOutput{Data: items}, nil
}

func (s *StubVolcanoCertLibrary) UpdateDomainWithContext(_ context.Context, input *volcanosdkwaf.UpdateDomainInput, _ ...request.Option) (*volcanosdkwaf.UpdateDomainOutput, error) {
	domain := volcengine.StringValue(input.Domain)
	certID := ""
	if input.CertificateID != nil {
		certID = strconv.FormatInt(int64(*input.CertificateID), 10)
	}
	if err := s.recordBind("waf", domain, certID); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if item, ok := s.wafDomains[domain]; ok {
		item.CertificateID = input.CertificateID
	}
	return &volcanosdkwaf.UpdateDomainOutput{}, nil
}

// ---------------------------------------------------------------------
// ALB listener library ( alb/nlb shared ) + listeners
// ---------------------------------------------------------------------

func (s *StubVolcanoCertLibrary) UploadCertificateWithContext(_ context.Context, _ *volcanosdkalb.UploadCertificateInput, _ ...request.Option) (*volcanosdkalb.UploadCertificateOutput, error) {
	s.mu.Lock()
	s.albUploads++
	id := "cert-alb-" + strconv.Itoa(s.albUploads)
	s.uploadInputs = append(s.uploadInputs, "alb:"+id)
	s.mu.Unlock()
	return &volcanosdkalb.UploadCertificateOutput{CertificateId: volcengine.String(id)}, nil
}

func (s *StubVolcanoCertLibrary) DescribeCertificatesWithContext(_ context.Context, input *volcanosdkalb.DescribeCertificatesInput, _ ...request.Option) (*volcanosdkalb.DescribeCertificatesOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []*volcanosdkalb.CertificateForDescribeCertificatesOutput
	for _, idPtr := range input.CertificateIds {
		if cert, ok := s.albCerts[*idPtr]; ok {
			items = append(items, cert)
		}
	}
	return &volcanosdkalb.DescribeCertificatesOutput{Certificates: items}, nil
}

func (s *StubVolcanoCertLibrary) DeleteCertificateWithContext(_ context.Context, input *volcanosdkalb.DeleteCertificateInput, _ ...request.Option) (*volcanosdkalb.DeleteCertificateOutput, error) {
	// alb/nlb share the ALB listener-library delete API.
	if err := s.recordDelete("alb", *input.CertificateId); err != nil {
		return nil, err
	}
	return &volcanosdkalb.DeleteCertificateOutput{}, nil
}

func (s *StubVolcanoCertLibrary) ModifyListenerAttributesWithContext(_ context.Context, input *volcanosdkalb.ModifyListenerAttributesInput, _ ...request.Option) (*volcanosdkalb.ModifyListenerAttributesOutput, error) {
	if err := s.recordBind("alb", volcengine.StringValue(input.ListenerId), volcengine.StringValue(input.CertificateId)); err != nil {
		return nil, err
	}
	return &volcanosdkalb.ModifyListenerAttributesOutput{}, nil
}

func (s *StubVolcanoCertLibrary) DescribeListenersWithContext(_ context.Context, input *volcanosdkalb.DescribeListenersInput, _ ...request.Option) (*volcanosdkalb.DescribeListenersOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.ListenerIds != nil {
		want := volcengine.StringValue(input.ListenerIds[0])
		wantLB := volcengine.StringValue(input.LoadBalancerId)
		for _, l := range s.albListeners {
			if volcengine.StringValue(l.ListenerId) != want {
				continue
			}
			if wantLB != "" && volcengine.StringValue(l.LoadBalancerId) != wantLB {
				continue
			}
			return &volcanosdkalb.DescribeListenersOutput{Listeners: []*volcanosdkalb.ListenerForDescribeListenersOutput{l}}, nil
		}
		return &volcanosdkalb.DescribeListenersOutput{}, nil
	}
	return &volcanosdkalb.DescribeListenersOutput{Listeners: s.albListeners}, nil
}

func (s *StubVolcanoCertLibrary) DescribeRulesWithContext(_ context.Context, _ *volcanosdkalb.DescribeRulesInput, _ ...request.Option) (*volcanosdkalb.DescribeRulesOutput, error) {
	return &volcanosdkalb.DescribeRulesOutput{}, nil
}

// ---------------------------------------------------------------------
// NLB listeners ( clb service NLB API )
// ---------------------------------------------------------------------

func (s *StubVolcanoCertLibrary) DescribeNLBListenersWithContext(_ context.Context, input *volcanosdkclb.DescribeNLBListenersInput, _ ...request.Option) (*volcanosdkclb.DescribeNLBListenersOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if input.ListenerIds != nil {
		want := volcengine.StringValue(input.ListenerIds[0])
		wantLB := volcengine.StringValue(input.LoadBalancerId)
		for _, l := range s.nlbListeners {
			if volcengine.StringValue(l.ListenerId) != want {
				continue
			}
			if wantLB != "" && volcengine.StringValue(l.LoadBalancerId) != wantLB {
				continue
			}
			return &volcanosdkclb.DescribeNLBListenersOutput{Listeners: []*volcanosdkclb.ListenerForDescribeNLBListenersOutput{l}}, nil
		}
		return &volcanosdkclb.DescribeNLBListenersOutput{}, nil
	}
	return &volcanosdkclb.DescribeNLBListenersOutput{Listeners: s.nlbListeners}, nil
}

func (s *StubVolcanoCertLibrary) ModifyNLBListenerAttributesWithContext(_ context.Context, input *volcanosdkclb.ModifyNLBListenerAttributesInput, _ ...request.Option) (*volcanosdkclb.ModifyNLBListenerAttributesOutput, error) {
	if err := s.recordBind("nlb", volcengine.StringValue(input.ListenerId), volcengine.StringValue(input.CertificateId)); err != nil {
		return nil, err
	}
	return &volcanosdkclb.ModifyNLBListenerAttributesOutput{}, nil
}
