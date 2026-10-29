package dns

import "testing"

// 权重校验：nil=未提供(放行)、0 与 100 为闭区间端点、越界拒绝。
// 语义来源：负载均衡解析权重通常 0-100（SDK types.NewDNSWeight 亦以 <=0 表达"无值"，
// 但 0 作为显式提交值仍属合法输入，故区间为 [0,100]）。
func TestValidateDNSWeight(t *testing.T) {
	w0, w100, wNeg, wOver := 0, 100, -1, 101
	cases := []struct {
		name string
		in   *int
		want error
	}{
		{"nil 表示未提供/不修改 → 放行", nil, nil},
		{"下界 0 合法", &w0, nil},
		{"上界 100 合法", &w100, nil},
		{"负数越界", &wNeg, ErrDNSWeightRange},
		{"超过 100 越界", &wOver, ErrDNSWeightRange},
	}
	for _, c := range cases {
		if got := ValidateDNSWeight(c.in); got != c.want {
			t.Errorf("%s: ValidateDNSWeight(%v) = %v, want %v", c.name, c.in, got, c.want)
		}
	}
}

// 不支持权重的云不得通过"可写"判定，避免前端出现能填却推不上去的假表单。
func TestDNSWeightWritabilityMatrix(t *testing.T) {
	if !DNSWeightProviderSupported("aliyun") {
		t.Error("aliyun 应标记为可读权重（DescribeDomainRecords 返回 Record.Weight）")
	}
	if DNSWeightProviderWritable("aliyun") {
		t.Error("aliyun 不应标记为可写（Add/UpdateDomainRecord 无 Weight 参数）")
	}
	for _, p := range []string{"huawei", "volcano", "unknown"} {
		if DNSWeightProviderWritable(p) {
			t.Errorf("%s 不应可写", p)
		}
	}
}
