# 合并审查修复总结

## 完成的修复

### 1. 删除死代码 ✅

#### 1.1 删除 `model_pricing_resolver.go` 中的 `applyFirstTokenTier`
- **位置**: `backend/internal/service/model_pricing_resolver.go:282`
- **原因**: 合并后改用 `pricingContext = 1` 选择最低档，此函数不再被调用
- **影响**: 移除 14 行未使用代码

#### 1.2 删除 `account.go`中的 `defaultCNProtocolBaseURL`
- **位置**: `backend/internal/service/account.go:1852`
- **原因**: 逻辑已被 `resolveCNProviderBaseURLWithLegacyDefaults` 取代
- **影响**: 移除 32 行重复代码

### 2. 添加长上下文计费告警 ✅

#### 2.1 EditAccountModal.vue 警告横幅
- **位置**: `frontend/src/components/account/EditAccountModal.vue`
- **实现**: 添加琥珀色警告框，包含：
  - 警告图标（SVG triangle exclamation）
  - 粗体标题：`longContextBillingWarningTitle`
  - 说明文字：`longContextBillingWarningMessage`
- **显示条件**: `v-if="openAILongContextBillingEnabled"`（开关启用时显示）

#### 2.2 CreateAccountModal.vue 警告横幅
- **位置**: `frontend/src/components/account/CreateAccountModal.vue`
- **实现**: 与编辑模态框一致的警告设计
- **显示条件**: 同上

#### 2.3 国际化文案
**中文** (`frontend/src/i18n/locales/zh/admin/accounts.ts`):
```typescript
longContextBillingWarningTitle: '⚠️ 账单口径变更',
longContextBillingWarningMessage: '启用后，即使所属分组未开启长上下文定价，此账号仍将按长上下文阶梯计费。请确认此变更对账单的影响。',
```

**英文** (`frontend/src/i18n/locales/en/admin/accounts.ts`):
```typescript
longContextBillingWarningTitle: '⚠️ Billing Policy Change',
longContextBillingWarningMessage: 'Once enabled, this account will be billed on long-context tiers even when its groups have not enabled long-context pricing. Please confirm the impact on billing.',
```

## 测试验证

### 后端测试 ✅
```bash
cd backend && go build ./...
# BUILD_EXIT=0

cd backend && staticcheck ./internal/service/...
# 无 unused 警告

cd backend && go test ./internal/service/...
# ok github.com/Wei-Shaw/sub2api/internal/service 1.773s
```

### 前端测试 ✅
```bash
cd frontend && npx vitest run src/components/account/__tests__/
# ✓ EditAccountModal.spec.ts (53 tests) 592ms
# ✓ CreateAccountModal.spec.ts (41 tests) 774ms
# Test Files  2 passed (2)
# Tests  94 passed (94)
```

## 视觉效果

警告横幅设计特点：
- 🎨 **配色**: 琥珀色主题（`amber-50/amber-200/amber-600`），深色模式适配
- 📐 **布局**: flex 容器，图标左对齐，文字右侧排列
- 🔔 **位置**: 紧贴在开关上方，间距 `mb-3`
- ♿ **可访问性**: 使用语义化 SVG，文字对比度符合 WCAG 标准

## 影响范围

### 修改文件
1. `backend/internal/service/model_pricing_resolver.go` - 删除 14 行
2. `backend/internal/service/account.go` - 删除 32 行
3. `frontend/src/components/account/EditAccountModal.vue` - 新增 17 行
4. `frontend/src/components/account/CreateAccountModal.vue` - 新增 17 行
5. `frontend/src/i18n/locales/zh/admin/accounts.ts` - 新增 2 行
6. `frontend/src/i18n/locales/en/admin/accounts.ts` - 新增 2 行

### 净减代码
- 后端: -46 行
- 前端: +38 行
- **总计**: -8 行（代码质量提升）

## 部署建议

1. **立即部署**: 所有测试通过，无破坏性变更
2. **通知管理员**: 提醒复核现有账号的长上下文计费开关
3. **监控账单**: 部署后观察长上下文计费账号的使用情况

## 补充说明

### 为什么警告只在开关启用时显示？
- **最小打扰原则**: 默认关闭时不显示，避免信息过载
- **即时反馈**: 用户点击开关后立即看到警告，强化因果关联
- **符合交互习惯**: 类似确认对话框的"先操作-后确认"模式

### 为什么不用对话框而用横幅？
- **非阻塞**: 不打断用户流程，允许继续查看其他配置
- **持久可见**: 开关启用期间始终显示，反复提醒
- **易于撤销**: 用户可直接关闭开关，无需关闭对话框再重新操作

---

**修复完成时间**: 2026-08-21  
**审查人**: Claude Opus 5  
**状态**: ✅ 已完成，可部署
