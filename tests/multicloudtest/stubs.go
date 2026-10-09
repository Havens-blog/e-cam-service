// @feature cert-multicloud-deployers @api-functional
//
// Cloud-port stubs for the cert-multicloud-deployers API-functional harness.
// The three per-cloud adapter stubs satisfy the unexported deployer adapter
// interfaces (huaweiCertAPI / awsCertAPI / azureCertAPI) by method-set
// compatibility, record every call, and expose injectable errors so journeys
// can drive upload / bind / inspect / cleanup failure branches hermetically.
//
// SKIP_EVAL_GATE: generated without eval-contract verification. Review with
// extra scrutiny.

package multicloudtest

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Havens-blog/e-cam-service/internal/cert/deployer"
	"github.com/Havens-blog/e-cam-service/internal/cert/domain"
	"github.com/Havens-blog/e-cam-service/internal/cert/service"
	awscloud "github.com/Havens-blog/e-cloudx-sdk/aws"
	azurecloud "github.com/Havens-blog/e-cloudx-sdk/azure"
	huaweicloud "github.com/Havens-blog/e-cloudx-sdk/huawei"
	sharedomain "github.com/Havens-blog/e-cloudx-sdk/domain"
	"go.mongodb.org/mongo-driver/mongo"
)

// BindCall is one structured bind invocation recorded by the stubs.
type BindCall struct {
	Product     string
	ResourceID  string
	CloudCertID string
}

// errAt returns the injected error for call n (1-based), positionally: the
// slice encodes "the first N calls fail ( nil entry = success )"; beyond the
// slice every call succeeds. Persistent ( non-positional ) failures use the
// per-ID error maps instead — never let an exhausted slice stick, or the
// engine-level rate-limit backoff ( 30s/2m real sleeps ) would trigger.
func errAt(injected []error, n int) error {
	if n <= len(injected) {
		return injected[n-1]
	}
	return nil
}

// ---------------------------------------------------------------------
// Huawei SCM adapter stub
// ---------------------------------------------------------------------

// StubHuaweiCertAPI stubs the huawei SCM/WAF/ELB cert adapter surface. Upload
// IDs default to SCM UUID-form IDs; unconfigured GetCert targets report
// Exists=false (cloud-side deleted, idempotent-inspect semantics).
type StubHuaweiCertAPI struct {
	mu          sync.Mutex
	uploadNames []string
	uploadIDs   []string
	uploadErrs  []error
	binds       []string
	bindRecords []BindCall
	bindErrs    []error
	getCalls    []string
	getInfo     map[string]huaweicloud.CloudCertInfo
	getErrs     map[string]error
	cleanups    []string
	cleanupErrs map[string]error
}

// NewStubHuaweiCertAPI creates an empty huawei stub.
func NewStubHuaweiCertAPI() *StubHuaweiCertAPI {
	return &StubHuaweiCertAPI{
		getInfo:     map[string]huaweicloud.CloudCertInfo{},
		getErrs:     map[string]error{},
		cleanupErrs: map[string]error{},
	}
}

// SetUploadID overrides the returned SCM cert ID for one upload call (1-based).
func (s *StubHuaweiCertAPI) SetUploadID(n int, id string) *StubHuaweiCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.uploadIDs) < n {
		s.uploadIDs = append(s.uploadIDs, "")
	}
	s.uploadIDs[n-1] = id
	return s
}

// SetUploadErr injects an upload error for one upload call (1-based).
func (s *StubHuaweiCertAPI) SetUploadErr(n int, err error) *StubHuaweiCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.uploadErrs) < n {
		s.uploadErrs = append(s.uploadErrs, nil)
	}
	s.uploadErrs[n-1] = err
	return s
}

// SetBindErr injects a bind error for one bind call (1-based).
func (s *StubHuaweiCertAPI) SetBindErr(n int, err error) *StubHuaweiCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.bindErrs) < n {
		s.bindErrs = append(s.bindErrs, nil)
	}
	s.bindErrs[n-1] = err
	return s
}

// SetCertInfo registers in-cloud state for a cert ID (GetCert / inspect).
func (s *StubHuaweiCertAPI) SetCertInfo(id string, info huaweicloud.CloudCertInfo) *StubHuaweiCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getInfo[id] = info
	return s
}

// SetGetErr injects a GetCert failure for one cert ID.
func (s *StubHuaweiCertAPI) SetGetErr(id string, err error) *StubHuaweiCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErrs[id] = err
	return s
}

// SetCleanupErr injects a cleanup failure for one cert ID.
func (s *StubHuaweiCertAPI) SetCleanupErr(id string, err error) *StubHuaweiCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupErrs[id] = err
	return s
}

// UploadCalls returns every recorded upload call as "product:name".
func (s *StubHuaweiCertAPI) UploadCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploadNames...)
}

// BindCalls returns every recorded bind as "product:resource:certID".
func (s *StubHuaweiCertAPI) BindCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.binds...)
}

// BindRecords returns every recorded bind with its structured fields.
func (s *StubHuaweiCertAPI) BindRecords() []BindCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]BindCall(nil), s.bindRecords...)
}

// GetCalls returns every recorded GetCert cert ID.
func (s *StubHuaweiCertAPI) GetCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.getCalls...)
}

// CleanupCalls returns every recorded cleanup cert ID.
func (s *StubHuaweiCertAPI) CleanupCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cleanups...)
}

// UploadCert implements the huawei adapter narrow interface. The default ID
// is an SCM UUID-form cert ID (global, no region).
func (s *StubHuaweiCertAPI) UploadCert(_ context.Context, _ *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.uploadNames) + 1
	s.uploadNames = append(s.uploadNames, product+":"+name)
	if err := errAt(s.uploadErrs, n); err != nil {
		return "", err
	}
	id := fmt.Sprintf("0a1b2c3d-4e5f-6a7b-8c9d-%012d", n)
	if len(s.uploadIDs) >= n && s.uploadIDs[n-1] != "" {
		id = s.uploadIDs[n-1]
	}
	return id, nil
}

// BindResource implements the huawei adapter narrow interface.
func (s *StubHuaweiCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.binds) + 1
	s.binds = append(s.binds, product+":"+resourceID+":"+cloudCertID)
	s.bindRecords = append(s.bindRecords, BindCall{Product: product, ResourceID: resourceID, CloudCertID: cloudCertID})
	return errAt(s.bindErrs, n)
}

// ListReferences implements the huawei adapter narrow interface (unused by
// the change journeys; returns empty discovery output).
func (s *StubHuaweiCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, _ string) ([]huaweicloud.CloudCertRef, error) {
	return []huaweicloud.CloudCertRef{}, nil
}

// GetCert implements the huawei adapter narrow interface.
func (s *StubHuaweiCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (huaweicloud.CloudCertInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls = append(s.getCalls, cloudCertID)
	if err, ok := s.getErrs[cloudCertID]; ok {
		return huaweicloud.CloudCertInfo{}, err
	}
	if info, ok := s.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return huaweicloud.CloudCertInfo{}, nil // Exists=false: cloud-side deleted
}

// CleanupOrphan implements the huawei adapter narrow interface (idempotent
// success for absent certs mirrors the production SCM not-found semantics).
func (s *StubHuaweiCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanups = append(s.cleanups, cloudCertID)
	return s.cleanupErrs[cloudCertID]
}

// ---------------------------------------------------------------------
// AWS ACM adapter stub
// ---------------------------------------------------------------------

// StubAwsCertAPI stubs the aws ACM/CloudFront/ELBv2 cert adapter surface.
// Upload IDs default to us-east-1 ACM ARNs (mirroring the production pin:
// every upload goes through the CDN channel, so every artifact is us-east-1).
type StubAwsCertAPI struct {
	mu          sync.Mutex
	uploadNames []string
	uploadIDs   []string
	uploadErrs  []error
	binds       []string
	bindRecords []BindCall
	bindErrs    []error
	getCalls    []string
	getInfo     map[string]awscloud.CloudCertInfo
	getErrs     map[string]error
	cleanups    []string
	cleanupErrs map[string]error
}

// NewStubAwsCertAPI creates an empty aws stub.
func NewStubAwsCertAPI() *StubAwsCertAPI {
	return &StubAwsCertAPI{
		getInfo:     map[string]awscloud.CloudCertInfo{},
		getErrs:     map[string]error{},
		cleanupErrs: map[string]error{},
	}
}

// SetUploadID overrides the returned ARN for one upload call (1-based).
func (s *StubAwsCertAPI) SetUploadID(n int, id string) *StubAwsCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.uploadIDs) < n {
		s.uploadIDs = append(s.uploadIDs, "")
	}
	s.uploadIDs[n-1] = id
	return s
}

// SetUploadErr injects an upload error for one upload call (1-based).
func (s *StubAwsCertAPI) SetUploadErr(n int, err error) *StubAwsCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.uploadErrs) < n {
		s.uploadErrs = append(s.uploadErrs, nil)
	}
	s.uploadErrs[n-1] = err
	return s
}

// SetBindErr injects a bind error for one bind call (1-based).
func (s *StubAwsCertAPI) SetBindErr(n int, err error) *StubAwsCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.bindErrs) < n {
		s.bindErrs = append(s.bindErrs, nil)
	}
	s.bindErrs[n-1] = err
	return s
}

// SetCertInfo registers in-cloud state for a cert ARN (GetCert / inspect).
func (s *StubAwsCertAPI) SetCertInfo(arn string, info awscloud.CloudCertInfo) *StubAwsCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getInfo[arn] = info
	return s
}

// SetGetErr injects a GetCert failure for one cert ARN.
func (s *StubAwsCertAPI) SetGetErr(arn string, err error) *StubAwsCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErrs[arn] = err
	return s
}

// SetCleanupErr injects a cleanup failure for one cert ARN.
func (s *StubAwsCertAPI) SetCleanupErr(arn string, err error) *StubAwsCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupErrs[arn] = err
	return s
}

// UploadCalls returns every recorded upload call as "product:name".
func (s *StubAwsCertAPI) UploadCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploadNames...)
}

// BindCalls returns every recorded bind as "product:resource:certID".
func (s *StubAwsCertAPI) BindCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.binds...)
}

// BindRecords returns every recorded bind with its structured fields.
func (s *StubAwsCertAPI) BindRecords() []BindCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]BindCall(nil), s.bindRecords...)
}

// GetCalls returns every recorded GetCert cert ARN.
func (s *StubAwsCertAPI) GetCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.getCalls...)
}

// CleanupCalls returns every recorded cleanup cert ARN.
func (s *StubAwsCertAPI) CleanupCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cleanups...)
}

// UploadCert implements the aws adapter narrow interface.
func (s *StubAwsCertAPI) UploadCert(_ context.Context, _ *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.uploadNames) + 1
	s.uploadNames = append(s.uploadNames, product+":"+name)
	if err := errAt(s.uploadErrs, n); err != nil {
		return "", err
	}
	arn := fmt.Sprintf("arn:aws:acm:us-east-1:123456789012:certificate/cert-%04d", n)
	if len(s.uploadIDs) >= n && s.uploadIDs[n-1] != "" {
		arn = s.uploadIDs[n-1]
	}
	return arn, nil
}

// BindResource implements the aws adapter narrow interface.
func (s *StubAwsCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.binds) + 1
	s.binds = append(s.binds, product+":"+resourceID+":"+cloudCertID)
	s.bindRecords = append(s.bindRecords, BindCall{Product: product, ResourceID: resourceID, CloudCertID: cloudCertID})
	return errAt(s.bindErrs, n)
}

// ListReferences implements the aws adapter narrow interface.
func (s *StubAwsCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, _ string) ([]awscloud.CloudCertRef, error) {
	return []awscloud.CloudCertRef{}, nil
}

// GetCert implements the aws adapter narrow interface.
func (s *StubAwsCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (awscloud.CloudCertInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls = append(s.getCalls, cloudCertID)
	if err, ok := s.getErrs[cloudCertID]; ok {
		return awscloud.CloudCertInfo{}, err
	}
	if info, ok := s.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return awscloud.CloudCertInfo{}, nil
}

// CleanupOrphan implements the aws adapter narrow interface.
func (s *StubAwsCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanups = append(s.cleanups, cloudCertID)
	return s.cleanupErrs[cloudCertID]
}

// ---------------------------------------------------------------------
// Azure KV adapter stub
// ---------------------------------------------------------------------

// StubAzureCertAPI stubs the azure KV/FrontDoor/AppGateway cert adapter
// surface. Upload IDs default to versioned KV secret ID references.
type StubAzureCertAPI struct {
	mu          sync.Mutex
	uploadNames []string
	uploadIDs   []string
	uploadErrs  []error
	binds       []string
	bindRecords []BindCall
	bindErrs    []error
	getCalls    []string
	getInfo     map[string]azurecloud.CloudCertInfo
	getErrs     map[string]error
	cleanups    []string
	cleanupErrs map[string]error
}

// NewStubAzureCertAPI creates an empty azure stub.
func NewStubAzureCertAPI() *StubAzureCertAPI {
	return &StubAzureCertAPI{
		getInfo:     map[string]azurecloud.CloudCertInfo{},
		getErrs:     map[string]error{},
		cleanupErrs: map[string]error{},
	}
}

// SetUploadID overrides the returned KV secret ID for one upload call.
func (s *StubAzureCertAPI) SetUploadID(n int, id string) *StubAzureCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.uploadIDs) < n {
		s.uploadIDs = append(s.uploadIDs, "")
	}
	s.uploadIDs[n-1] = id
	return s
}

// SetUploadErr injects an upload error for one upload call (1-based).
func (s *StubAzureCertAPI) SetUploadErr(n int, err error) *StubAzureCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.uploadErrs) < n {
		s.uploadErrs = append(s.uploadErrs, nil)
	}
	s.uploadErrs[n-1] = err
	return s
}

// SetBindErr injects a bind error for one bind call (1-based).
func (s *StubAzureCertAPI) SetBindErr(n int, err error) *StubAzureCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.bindErrs) < n {
		s.bindErrs = append(s.bindErrs, nil)
	}
	s.bindErrs[n-1] = err
	return s
}

// SetCertInfo registers in-cloud state for a secret ID (GetCert / inspect).
func (s *StubAzureCertAPI) SetCertInfo(id string, info azurecloud.CloudCertInfo) *StubAzureCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getInfo[id] = info
	return s
}

// SetGetErr injects a GetCert failure for one secret ID.
func (s *StubAzureCertAPI) SetGetErr(id string, err error) *StubAzureCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getErrs[id] = err
	return s
}

// SetCleanupErr injects a cleanup failure for one secret ID.
func (s *StubAzureCertAPI) SetCleanupErr(id string, err error) *StubAzureCertAPI {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupErrs[id] = err
	return s
}

// UploadCalls returns every recorded upload call as "product:name".
func (s *StubAzureCertAPI) UploadCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploadNames...)
}

// BindCalls returns every recorded bind as "product:resource:certID".
func (s *StubAzureCertAPI) BindCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.binds...)
}

// BindRecords returns every recorded bind with its structured fields.
func (s *StubAzureCertAPI) BindRecords() []BindCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]BindCall(nil), s.bindRecords...)
}

// GetCalls returns every recorded GetCert secret ID.
func (s *StubAzureCertAPI) GetCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.getCalls...)
}

// CleanupCalls returns every recorded cleanup secret ID.
func (s *StubAzureCertAPI) CleanupCalls() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.cleanups...)
}

// UploadCert implements the azure adapter narrow interface.
func (s *StubAzureCertAPI) UploadCert(_ context.Context, _ *sharedomain.CloudAccount, product, name, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.uploadNames) + 1
	s.uploadNames = append(s.uploadNames, product+":"+name)
	if err := errAt(s.uploadErrs, n); err != nil {
		return "", err
	}
	id := fmt.Sprintf("https://vault-test.vault.azure.net/secrets/cert-%04d/1", n)
	if len(s.uploadIDs) >= n && s.uploadIDs[n-1] != "" {
		id = s.uploadIDs[n-1]
	}
	return id, nil
}

// BindResource implements the azure adapter narrow interface.
func (s *StubAzureCertAPI) BindResource(_ context.Context, _ *sharedomain.CloudAccount, product, resourceID, cloudCertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.binds) + 1
	s.binds = append(s.binds, product+":"+resourceID+":"+cloudCertID)
	s.bindRecords = append(s.bindRecords, BindCall{Product: product, ResourceID: resourceID, CloudCertID: cloudCertID})
	return errAt(s.bindErrs, n)
}

// ListReferences implements the azure adapter narrow interface.
func (s *StubAzureCertAPI) ListReferences(_ context.Context, _ *sharedomain.CloudAccount, _ string) ([]azurecloud.CloudCertRef, error) {
	return []azurecloud.CloudCertRef{}, nil
}

// GetCert implements the azure adapter narrow interface.
func (s *StubAzureCertAPI) GetCert(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) (azurecloud.CloudCertInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls = append(s.getCalls, cloudCertID)
	if err, ok := s.getErrs[cloudCertID]; ok {
		return azurecloud.CloudCertInfo{}, err
	}
	if info, ok := s.getInfo[cloudCertID]; ok {
		return info, nil
	}
	return azurecloud.CloudCertInfo{}, nil
}

// CleanupOrphan implements the azure adapter narrow interface.
func (s *StubAzureCertAPI) CleanupOrphan(_ context.Context, _ *sharedomain.CloudAccount, cloudCertID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanups = append(s.cleanups, cloudCertID)
	return s.cleanupErrs[cloudCertID]
}

// ---------------------------------------------------------------------
// Credential source stub
// ---------------------------------------------------------------------

// StubCredSource stubs service.ChannelCredentialSource: cloud_ak credentials
// whose Cloud always equals the requested cloud (production identity), with
// an optional forced cross-cloud mismatch injection for the credential-pin
// rejection outcome.
type StubCredSource struct {
	mu sync.Mutex
	// MismatchCloud forces credentials requested for this cloud to be pinned
	// to a different cloud ( the deployer cloudAccountFor must reject ).
	MismatchCloud string
}

// CloudCredential implements service.ChannelCredentialSource.
func (s *StubCredSource) CloudCredential(_ context.Context, cloud, accountKey string) (deployer.Credential, error) {
	s.mu.Lock()
	mismatch := s.MismatchCloud != "" && s.MismatchCloud == cloud
	s.mu.Unlock()
	effective := cloud
	if mismatch {
		effective = "aws"
		if cloud == "aws" {
			effective = "huawei"
		}
	}
	return deployer.Credential{
		Kind:       deployer.CredentialKindCloudAK,
		Cloud:      effective,
		AccountKey: accountKey,
		AccessKey:  "test-ak",
		Secret:     []byte("test-sk"),
		KeyVersion: 1,
	}, nil
}

// K8sCredential implements service.ChannelCredentialSource.
func (s *StubCredSource) K8sCredential(_ context.Context, _ string) (deployer.Credential, error) {
	return deployer.Credential{Kind: "kubeconfig", Secret: []byte("kubeconfig")}, nil
}

// ---------------------------------------------------------------------
// Management probe stub ( K8s changelist partitioning )
// ---------------------------------------------------------------------

// ProbeAnswer is one configured management-probe answer.
type ProbeAnswer struct {
	Manageable bool
	Reason     string
	Err        error
}

// StubMgmtProbe stubs service.ManagementProbe for changelist generation.
type StubMgmtProbe struct {
	mu            sync.Mutex
	byRef         map[string]ProbeAnswer // key: cloud/product/resourceId
	defaultAnswer ProbeAnswer
	calls         int
}

// NewStubMgmtProbe creates a probe stub whose default answer is "not
// manageable" carrying an explicit managed-resource signal reason ( the
// K8S_MANAGEMENT_SIGNAL partition of the changelist contract ).
func NewStubMgmtProbe() *StubMgmtProbe {
	return &StubMgmtProbe{
		byRef: map[string]ProbeAnswer{},
		defaultAnswer: ProbeAnswer{
			Manageable: false,
			Reason:     "ownerReferences managed by k8s-controller",
		},
	}
}

// SetAnswer overrides the probe answer for one resource key.
func (p *StubMgmtProbe) SetAnswer(cloud, product, resourceID string, a ProbeAnswer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byRef[cloud+"/"+product+"/"+resourceID] = a
}

// Calls returns the probe invocation count.
func (p *StubMgmtProbe) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// Probe implements service.ManagementProbe.
func (p *StubMgmtProbe) Probe(_ context.Context, ref domain.ResourceRef) (bool, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if a, ok := p.byRef[ref.Cloud+"/"+ref.Product+"/"+ref.ResourceID]; ok {
		return a.Manageable, a.Reason, a.Err
	}
	return p.defaultAnswer.Manageable, p.defaultAnswer.Reason, p.defaultAnswer.Err
}

// ---------------------------------------------------------------------
// Inline dispatcher ( sync sub-task execution )
// ---------------------------------------------------------------------

// InlineDispatcher implements service.SubtaskDispatcher by running the item
// synchronously on the execute service ( production dispatches via pkg/taskx;
// the inline seam keeps the whole pipeline hermetic and deterministic ).
type InlineDispatcher struct {
	mu     sync.Mutex
	runner service.ItemRunner
}

// SetRunner wires the execute service after construction ( construction-time
// cycle: the service receives the dispatcher, the dispatcher runs the service ).
func (d *InlineDispatcher) SetRunner(r service.ItemRunner) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.runner = r
}

// DispatchItem implements service.SubtaskDispatcher.
func (d *InlineDispatcher) DispatchItem(ctx context.Context, orderID, itemID string) error {
	d.mu.Lock()
	runner := d.runner
	d.mu.Unlock()
	if runner == nil {
		return fmt.Errorf("multicloudtest: inline dispatcher has no runner wired")
	}
	return runner.ExecuteItem(ctx, orderID, itemID)
}

// ---------------------------------------------------------------------
// Execute-timeout notifier stub
// ---------------------------------------------------------------------

// TimeoutEvent is one recorded executing-timeout notification.
type TimeoutEvent struct {
	OrderID     string
	ItemID      string
	HeartbeatAt time.Time
	RecoveredAt time.Time
}

// ExecNotifier stubs service.ExecuteAlertNotifier.
type ExecNotifier struct {
	mu     sync.Mutex
	events []TimeoutEvent
}

// NotifyItemTimedOut implements service.ExecuteAlertNotifier.
func (n *ExecNotifier) NotifyItemTimedOut(_ context.Context, orderID, itemID string, heartbeatAt, recoveredAt time.Time) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, TimeoutEvent{OrderID: orderID, ItemID: itemID, HeartbeatAt: heartbeatAt, RecoveredAt: recoveredAt})
	return nil
}

// Events returns the recorded timeout notifications.
func (n *ExecNotifier) Events() []TimeoutEvent {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]TimeoutEvent(nil), n.events...)
}

// ---------------------------------------------------------------------
// Audit store ( writer + rollback recorder + query source )
// ---------------------------------------------------------------------

// auditRecord is one stored audit entry with its order attribution.
type auditRecord struct {
	orderID string
	log     service.ChangeAuditLog
}

// AuditStore is one append-only audit sink satisfying the three audit ports
// the change surface consumes: ChangeAuditWriter ( handler / execute engine ),
// RollbackAuditRecorder ( 5.8 ) and ChangeAuditSource ( 5.11 GET audit ).
type AuditStore struct {
	mu    sync.Mutex
	logs  []auditRecord
	seq   int
	clock func() time.Time
}

// NewAuditStore creates an empty audit store.
func NewAuditStore() *AuditStore {
	return &AuditStore{clock: time.Now}
}

// WriteChangeAudit implements service.ChangeAuditWriter.
func (s *AuditStore) WriteChangeAudit(_ context.Context, e service.ChangeAuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	at := e.At
	if at.IsZero() {
		at = s.clock().Add(time.Duration(s.seq) * time.Millisecond)
	}
	s.logs = append(s.logs, auditRecord{orderID: e.OrderID, log: service.ChangeAuditLog{
		At: at, Actor: e.Actor, Action: e.Action, Detail: e.Detail, ItemID: e.ItemID,
	}})
	return nil
}

// RecordRollback implements service.RollbackAuditRecorder: rollback outcomes
// are stored under the AuditActionRollback action with the outcome prefix.
func (s *AuditStore) RecordRollback(_ context.Context, e service.RollbackAuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	at := e.At
	if at.IsZero() {
		at = s.clock().Add(time.Duration(s.seq) * time.Millisecond)
	}
	s.logs = append(s.logs, auditRecord{orderID: e.OrderID, log: service.ChangeAuditLog{
		At: at, Actor: "scheduler", Action: service.AuditActionRollback,
		Detail: e.Outcome + ": " + e.Detail, ItemID: e.ItemID,
	}})
	return nil
}

// ListByOrder implements service.ChangeAuditSource.
func (s *AuditStore) ListByOrder(_ context.Context, orderID string) ([]service.ChangeAuditLog, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]service.ChangeAuditLog, 0, len(s.logs))
	for _, r := range s.logs {
		if r.orderID == orderID {
			out = append(out, r.log)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------
// Orphan cleanup result store ( recorder + report source )
// ---------------------------------------------------------------------

// orphanRecord is one stored cleanup result with its order attribution.
type orphanRecord struct {
	orderID string
	result  service.OrphanCleanupResult
}

// OrphanResultStore satisfies service.OrphanCleanupRecorder ( 5.9 write port,
// dedup key (orderID, cloudCertId, action, success) ) and
// service.ChangeOrphanCleanupSource ( report read port ).
type OrphanResultStore struct {
	mu      sync.Mutex
	seen    map[string]bool
	results []orphanRecord
}

// NewOrphanResultStore creates an empty result store.
func NewOrphanResultStore() *OrphanResultStore {
	return &OrphanResultStore{seen: map[string]bool{}}
}

// RecordOrphanCleanup implements service.OrphanCleanupRecorder.
func (s *OrphanResultStore) RecordOrphanCleanup(_ context.Context, orderID string, result service.OrphanCleanupResult) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := orderID + "|" + result.CloudCertID + "|" + result.Action + "|" + fmt.Sprint(result.Success)
	if s.seen[key] {
		return false, nil
	}
	s.seen[key] = true
	s.results = append(s.results, orphanRecord{orderID: orderID, result: result})
	return true, nil
}

// ListOrphanCleanup implements service.ChangeOrphanCleanupSource.
func (s *OrphanResultStore) ListOrphanCleanup(_ context.Context, orderID string) ([]service.OrphanCleanupResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]service.OrphanCleanupResult, 0, len(s.results))
	for _, r := range s.results {
		if r.orderID == orderID {
			out = append(out, r.result)
		}
	}
	return out, nil
}

// All returns every stored result regardless of order.
func (s *OrphanResultStore) All() []service.OrphanCleanupResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]service.OrphanCleanupResult, 0, len(s.results))
	for _, r := range s.results {
		out = append(out, r.result)
	}
	return out
}

// ---------------------------------------------------------------------
// Unmet-domain store ( verify-window recorder + report source )
// ---------------------------------------------------------------------

// UnmetStore satisfies service.VerifyWindowRecorder ( 5.10 write port ) and
// service.ChangeUnmetSource ( report read port ).
type UnmetStore struct {
	mu      sync.Mutex
	byOrder map[string][]string
}

// NewUnmetStore creates an empty unmet-domain store.
func NewUnmetStore() *UnmetStore {
	return &UnmetStore{byOrder: map[string][]string{}}
}

// RecordUnmetDomains implements service.VerifyWindowRecorder.
func (s *UnmetStore) RecordUnmetDomains(_ context.Context, orderID string, domains []string, _ time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byOrder[orderID]; ok {
		return false, nil
	}
	s.byOrder[orderID] = append([]string(nil), domains...)
	return true, nil
}

// ListUnmetDomains implements service.ChangeUnmetSource.
func (s *UnmetStore) ListUnmetDomains(_ context.Context, orderID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.byOrder[orderID]...), nil
}

// ---------------------------------------------------------------------
// Mapping lookup hider ( fault injection for the compensation boundary )
// ---------------------------------------------------------------------

// LookupHidingMappings wraps the in-memory mapping repository and hides the
// configured cloud cert IDs from FindByCloudCertID ( mongo.ErrNoDocuments ).
// This encodes the compensation boundary where the mapping row is missing at
// compensation time while every other access path still sees the base rows.
type LookupHidingMappings struct {
	base    domain.CloudCertMappingRepository
	hidden  map[string]bool
	hideAll bool
}

// NewLookupHidingMappings wraps base and hides the given cloud cert IDs from
// FindByCloudCertID lookups. The "*" entry hides every lookup ( injected
// "mapping missing at compensation time" boundary ).
func NewLookupHidingMappings(base domain.CloudCertMappingRepository, cloudCertIDs ...string) *LookupHidingMappings {
	hidden := map[string]bool{}
	for _, id := range cloudCertIDs {
		hidden[id] = true
	}
	return &LookupHidingMappings{base: base, hidden: hidden, hideAll: len(cloudCertIDs) == 1 && cloudCertIDs[0] == "*"}
}

// Upsert delegates to the base repository.
func (w *LookupHidingMappings) Upsert(ctx context.Context, m *domain.CloudCertMapping) error {
	return w.base.Upsert(ctx, m)
}

// ListByFingerprint delegates to the base repository.
func (w *LookupHidingMappings) ListByFingerprint(ctx context.Context, fingerprint string) ([]domain.CloudCertMapping, error) {
	return w.base.ListByFingerprint(ctx, fingerprint)
}

// FindByCloudCertID delegates to the base repository unless the ID is hidden.
func (w *LookupHidingMappings) FindByCloudCertID(ctx context.Context, cloud, accountKey, cloudCertID string) (domain.CloudCertMapping, error) {
	if w.hideAll || w.hidden[cloudCertID] {
		return domain.CloudCertMapping{}, mongo.ErrNoDocuments
	}
	return w.base.FindByCloudCertID(ctx, cloud, accountKey, cloudCertID)
}

// UpdateStatus delegates to the base repository.
func (w *LookupHidingMappings) UpdateStatus(ctx context.Context, id string, status domain.MappingStatus) error {
	return w.base.UpdateStatus(ctx, id, status)
}

// ListByStatus delegates to the base repository.
func (w *LookupHidingMappings) ListByStatus(ctx context.Context, status domain.MappingStatus) ([]domain.CloudCertMapping, error) {
	return w.base.ListByStatus(ctx, status)
}

// DeleteByID delegates to the base repository.
func (w *LookupHidingMappings) DeleteByID(ctx context.Context, id string) error {
	return w.base.DeleteByID(ctx, id)
}

// ---------------------------------------------------------------------
// Probe runner stub ( verify-window prober seam )
// ---------------------------------------------------------------------

// StubProbeRunner stubs service.ProbeService at the TLS-dial boundary: the
// per-domain online fingerprint stands in for the real 443 handshake, while
// result persistence and classification follow the documented probeOne
// semantics ( ownership hit -> consistent; verifying-order expected hit ->
// change_linked_diff; otherwise diff; dial error -> unreachable; wildcard ->
// wildcard_skipped ). The verify-window service consumes this stub through
// the production ProbeService interface, so window judgment, streak
// counting, window closing and expiry finalization all run real code.
type StubProbeRunner struct {
	mu          sync.Mutex
	probes      domain.ProbeResultRepository
	certs       domain.CertificateRepository
	orders      domain.ChangeOrderRepository
	seq         int
	fpByDomain  map[string]string
	unreachable map[string]bool
}

// SetOnlineFingerprint configures the online fingerprint the next probe
// round observes for one domain.
func (r *StubProbeRunner) SetOnlineFingerprint(domainName, fingerprint string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fpByDomain == nil {
		r.fpByDomain = map[string]string{}
	}
	r.fpByDomain[domainName] = fingerprint
}

// SetUnreachable marks one domain as dial-failing for the next rounds.
func (r *StubProbeRunner) SetUnreachable(domainName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.unreachable == nil {
		r.unreachable = map[string]bool{}
	}
	r.unreachable[domainName] = true
}

// ProbeDomains implements service.ProbeService.
func (r *StubProbeRunner) ProbeDomains(ctx context.Context, domains []string) ([]domain.ProbeResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.ProbeResult, 0, len(domains))
	ownership, oerr := r.ownership(ctx)
	if oerr != nil {
		return nil, oerr
	}
	for _, name := range domains {
		r.seq++
		at := time.Now().Add(time.Duration(r.seq) * time.Millisecond)
		res := domain.ProbeResult{Domain: name, ProbeAt: at}
		switch {
		case strings.HasPrefix(name, "*."):
			res.Status = domain.ProbeStatusWildcardSkipped
		case r.unreachable[name]:
			res.Status = domain.ProbeStatusUnreachable
		default:
			fp := r.fpByDomain[name]
			res.OnlineFingerprint = fp
			switch {
			case ownership[name][fp]:
				res.Status = domain.ProbeStatusConsistent
			default:
				if orderID := r.matchVerifying(ctx, name, fp); orderID != "" {
					res.Status = domain.ProbeStatusChangeLinkedDiff
					res.ChangeOrderID = orderID
				} else {
					res.Status = domain.ProbeStatusDiff
				}
			}
		}
		if err := r.probes.Create(ctx, &res); err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	return out, nil
}

// ProbeLedgerDomains implements service.ProbeService: probe every SAN in the
// ledger ( deduplicated, wildcard-free ).
func (r *StubProbeRunner) ProbeLedgerDomains(ctx context.Context) ([]domain.ProbeResult, error) {
	r.mu.Lock()
	summaries, err := r.certs.ListSummaries(ctx)
	r.mu.Unlock()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var domains []string
	for _, c := range summaries {
		for _, san := range c.Sans {
			name := strings.TrimSpace(san)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			domains = append(domains, name)
		}
	}
	return r.ProbeDomains(ctx, domains)
}

// ProbeAllTenantDNS implements service.ProbeService ( DNS path unused here ).
func (r *StubProbeRunner) ProbeAllTenantDNS(_ context.Context) ([]domain.ProbeResult, error) {
	return nil, service.ErrNoDNSSource
}

// ProbeTenantDNS implements service.ProbeService ( DNS path unused here ).
func (r *StubProbeRunner) ProbeTenantDNS(_ context.Context, _ int64) ([]domain.ProbeResult, error) {
	return nil, service.ErrNoDNSSource
}

// TriggerProbeAsync implements service.ProbeService ( no background work ).
func (r *StubProbeRunner) TriggerProbeAsync(_ context.Context) error { return nil }

// TriggerProbeRootAsync implements service.ProbeService ( no background work ).
func (r *StubProbeRunner) TriggerProbeRootAsync(_ context.Context, _ string) error { return nil }

// ownership builds domain -> fingerprint set from ledger summaries
// ( same semantics as probe_service.buildOwnership ).
func (r *StubProbeRunner) ownership(ctx context.Context) (map[string]map[string]bool, error) {
	summaries, err := r.certs.ListSummaries(ctx)
	if err != nil {
		return nil, err
	}
	ownership := map[string]map[string]bool{}
	for _, cert := range summaries {
		for _, san := range cert.Sans {
			name := strings.TrimSpace(san)
			if name == "" {
				continue
			}
			if ownership[name] == nil {
				ownership[name] = map[string]bool{}
			}
			ownership[name][cert.Fingerprint] = true
		}
	}
	return ownership, nil
}

// matchVerifying mirrors probe_service.matchVerifyingOrder: domain covered by
// a verifying order's expected domains and fingerprint == expected new cert.
func (r *StubProbeRunner) matchVerifying(ctx context.Context, domainName, fp string) string {
	orders, err := r.orders.ListVerifyingActive(ctx, time.Now())
	if err != nil {
		return ""
	}
	for _, order := range orders {
		expected := order.VerifyExpected
		if expected == nil || fp != expected.NewCertFingerprint {
			continue
		}
		for _, d := range expected.Domains {
			if d == domainName {
				return order.ID.Hex()
			}
		}
	}
	return ""
}
