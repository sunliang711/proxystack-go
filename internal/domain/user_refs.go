package domain

import "fmt"

// ResolveStackSetUserRefs 展开所有 stack 的 user_refs，并返回可直接用于生成器的 stack set。
func ResolveStackSetUserRefs(stackSet StackSet) (StackSet, error) {
	resolved := stackSet
	resolved.Stacks = append([]Stack(nil), stackSet.Stacks...)
	for stackIndex := range resolved.Stacks {
		stack, err := ResolveStackUserRefs(stackSet.Config, resolved.Stacks[stackIndex])
		if err != nil {
			return StackSet{}, err
		}
		resolved.Stacks[stackIndex] = stack
	}
	return resolved, nil
}

// ResolveStackUserRefs 展开单个 stack 内的 user_refs，适用于候选 stack 写入前校验。
func ResolveStackUserRefs(config GlobalConfig, stack Stack) (Stack, error) {
	profiles := userProfileIndex(config.Users)
	resolved := stack
	resolved.Xray.Inbounds = append([]Inbound(nil), stack.Xray.Inbounds...)
	for inboundIndex := range resolved.Xray.Inbounds {
		inbound := resolved.Xray.Inbounds[inboundIndex]
		if len(inbound.UserRefs) == 0 {
			continue
		}
		users, err := resolveInboundUserRefs(profiles, inbound)
		if err != nil {
			return Stack{}, fmt.Errorf("stacks.%s.xray.inbounds[%d].user_refs: %w", stack.Name, inboundIndex, err)
		}
		inbound.Users = users
		inbound.UserRefs = nil
		if inbound.fields == nil {
			inbound.fields = map[string]bool{}
			inbound.fields["sub"] = true
			if inbound.UDP {
				inbound.fields["udp"] = true
			}
		}
		inbound.fields["user_refs"] = true
		if err := inbound.Validate(); err != nil {
			return Stack{}, fmt.Errorf("stacks.%s.xray.inbounds[%d]: %w", stack.Name, inboundIndex, err)
		}
		resolved.Xray.Inbounds[inboundIndex] = inbound
	}
	return resolved, nil
}

// userProfileIndex 按 (user, profile) 建立全局用户档案索引。
func userProfileIndex(users []UserProfile) map[string]UserProfile {
	profiles := make(map[string]UserProfile, len(users))
	for _, user := range users {
		user.Profile = NormalizeUserProfile(user.Profile)
		profiles[UserProfileKey(user.User, user.Profile)] = user
	}
	return profiles
}

// resolveInboundUserRefs 将单个 inbound 的 user_refs 合并为内部 InboundUser 列表。
func resolveInboundUserRefs(profiles map[string]UserProfile, inbound Inbound) ([]InboundUser, error) {
	users := make([]InboundUser, 0, len(inbound.UserRefs))
	for _, ref := range inbound.UserRefs {
		ref.Profile = NormalizeUserProfile(ref.Profile)
		profile, ok := profiles[ref.Key()]
		if !ok {
			return nil, fmt.Errorf("config user profile does not exist: user=%s profile=%s", ref.User, ref.Profile)
		}
		user := mergeUserRef(profile, ref)
		if err := validateResolvedInboundUser(inbound.Protocol, user); err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, nil
}

// mergeUserRef 合并全局用户档案和 inbound 级覆盖字段。
func mergeUserRef(profile UserProfile, ref InboundUserRef) InboundUser {
	return InboundUser{
		User:            ref.User,
		Profile:         NormalizeUserProfile(ref.Profile),
		UUID:            firstNonEmpty(ref.UUID, profile.UUID),
		Password:        firstNonEmpty(ref.Password, profile.Password),
		Method:          firstNonEmpty(ref.Method, profile.Method),
		Cipher:          firstNonEmpty(ref.Cipher, profile.Cipher),
		Email:           firstNonEmpty(ref.Email, profile.Email),
		Remark:          firstNonEmpty(ref.Remark, profile.Remark),
		DisplayTemplate: firstNonEmpty(ref.DisplayTemplate, profile.DisplayTemplate),
		Tag:             firstNonEmpty(ref.Tag, profile.Tag),
	}
}

// validateResolvedInboundUser 按 inbound 协议校验展开后的用户必需凭据。
func validateResolvedInboundUser(protocol string, user InboundUser) error {
	switch protocol {
	case "vmess":
		if user.UUID == "" {
			return fmt.Errorf("uuid is required for vmess user_ref: user=%s profile=%s", user.User, user.ProfileOrDefault())
		}
	case "shadowsocks":
		if user.Password == "" {
			return fmt.Errorf("password is required for shadowsocks user_ref: user=%s profile=%s", user.User, user.ProfileOrDefault())
		}
	}
	return nil
}

// firstNonEmpty 返回第一个非空字符串，用于用户档案覆盖合并。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
