package domain

import "testing"

// TestResolveStackUserRefsPreservesProgrammaticUserRefSource 验证内存构造的 user_refs 展开后仍保留来源语义。
func TestResolveStackUserRefsPreservesProgrammaticUserRefSource(t *testing.T) {
	stack := Stack{
		Name: "edge",
		Xrelay: XrelayConfig{
			Inbounds: []Inbound{{
				Name:     "vmess",
				Protocol: "vmess",
				Port:     24001,
				Sub:      true,
				Network:  "raw",
				Remark:   "Shared",
				UserRefs: []InboundUserRef{{
					User: "alice",
				}},
			}},
		},
	}
	config := GlobalConfig{Users: []UserProfile{{
		User: "alice",
		UUID: "11111111-1111-4111-8111-111111111111",
	}}}

	resolved, err := ResolveStackUserRefs(config, stack)

	if err != nil {
		t.Fatalf("ResolveStackUserRefs() error = %v", err)
	}
	inbound := resolved.Xrelay.Inbounds[0]
	if !inbound.UsesUserRefs() {
		t.Fatal("expected resolved inbound to preserve user_refs source")
	}
	if len(inbound.UserRefs) != 0 {
		t.Fatalf("expected user_refs to be consumed, got %d", len(inbound.UserRefs))
	}
	if len(inbound.Users) != 1 {
		t.Fatalf("expected one resolved user, got %d", len(inbound.Users))
	}
}
