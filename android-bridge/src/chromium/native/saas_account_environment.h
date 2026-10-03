#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_ACCOUNT_ENVIRONMENT_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_ACCOUNT_ENVIRONMENT_H_

#include <map>
#include <memory>
#include <string>

#include "base/command_line.h"
#include "base/hash/hash.h"
#include "base/memory/weak_ptr.h"
#include "base/strings/string_number_conversions.h"
#include "base/supports_user_data.h"
#include "base/values.h"
#include "chrome/common/chrome_switches.h"
#include "chrome/browser/ui/android/tab_model/saas_account_state.h"
#include "components/ungoogled/ungoogled_switches.h"
#include "content/public/browser/browser_context.h"
#include "content/public/browser/site_instance.h"
#include "content/public/browser/storage_partition_config.h"
#include "content/public/browser/web_contents.h"
#include "content/public/browser/web_contents_observer.h"
#include "net/proxy_resolution/proxy_config.h"
#include "services/network/public/mojom/network_context.mojom.h"

namespace chrome::android {

// 账号分区在 WebContents 创建之前确定，不能先建普通 Tab 再修改 Cookie 所属环境。
// 此类只保存本地原生环境信息，不保存 SaaS 登录、工作区或业务权限。
class SaasAccountEnvironment final : public base::SupportsUserData::Data,
                                     public content::WebContentsObserver {
 public:
  static constexpr char kPartitionDomain[] = "saasandroid";
  ~SaasAccountEnvironment() override = default;

  static bool IsValidAccountId(const std::string& value) {
    return SaasAccountState::IsValidAccountId(value);
  }

  static bool IsValidSeed(const std::string& value) {
    return SaasAccountState::IsValidSeed(value);
  }

  static bool ParseProxy(const std::string& value,
                         net::ProxyConfig::ProxyRules* rules) {
    if (value.empty()) return true;
    if (value.size() > 2048 || value.find_first_of(";,=@\r\n\0", 0, 7) != std::string::npos) return false;
    rules->ParseFromString(value);
    return rules->type == net::ProxyConfig::ProxyRules::Type::PROXY_LIST &&
           rules->single_proxies.size() == 1;
  }

  static std::unique_ptr<content::WebContents> Create(
      content::BrowserContext* browser_context,
      const std::string& account_id,
      const std::string& proxy_rules,
      const std::string& fingerprint_seed,
      std::string* error,
      bool initially_hidden = true,
      bool no_renderer = false) {
    net::ProxyConfig::ProxyRules proxy;
    if (!browser_context || browser_context->IsOffTheRecord() ||
        !IsValidAccountId(account_id) || !ParseProxy(proxy_rules, &proxy) ||
        (!fingerprint_seed.empty() && !IsValidSeed(fingerprint_seed))) {
      SetError(error, "账号标识、代理或指纹参数无效，不能创建隔离环境");
      return nullptr;
    }
    Registry& registry = GetRegistry(browser_context);
    const std::string seed = fingerprint_seed.empty()
        ? base::NumberToString(base::PersistentHash(account_id) & 0x7fffffffU)
        : fingerprint_seed;
    auto existing = registry.entries.find(account_id);
    if (existing != registry.entries.end()) {
      if (existing->second.live) {
        SetError(error, "该账号已有隔离环境，不能创建共享同一分区的重复 Tab");
        return nullptr;
      }
      if (existing->second.proxy != proxy_rules || existing->second.seed != seed) {
        // 已缓存的 NetworkContext/渲染配置不能假装因关闭 Tab 就完成切换。
        SetError(error, "本机该账号分区已有不同配置，请重启浏览器后再恢复");
        return nullptr;
      }
    } else {
      existing = registry.entries.emplace(account_id, Entry{proxy_rules, seed, {}}).first;
    }
    const auto config = content::StoragePartitionConfig::Create(
        browser_context, kPartitionDomain, account_id, false);
    content::WebContents::CreateParams params(browser_context);
    params.site_instance = content::SiteInstance::CreateForFixedStoragePartition(
        browser_context, GURL("about:blank"), config);
    params.initially_hidden = initially_hidden;
    if (no_renderer) {
      params.desired_renderer_state = content::WebContents::CreateParams::kNoRendererProcess;
    }
    auto contents = content::WebContents::Create(params);
    if (!contents || contents->GetSiteInstance()->GetStoragePartitionConfig() != config) {
      SetError(error, "未能建立固定持久化分区，账号环境创建已停止");
      return nullptr;
    }
    // 即使创建中途失败也保留该分区的配置约束，避免复用已经缓存的不同网络设置。
    auto environment = std::unique_ptr<SaasAccountEnvironment>(
        new SaasAccountEnvironment(contents.get(), account_id, proxy_rules, seed));
    existing->second.live = environment->weak_factory_.GetWeakPtr();
    contents->SetUserData(&kEnvironmentKey, std::move(environment));
    return contents;
  }

  static SaasAccountEnvironment* FromWebContents(content::WebContents* contents) {
    if (!contents) return nullptr;
    auto* result = static_cast<SaasAccountEnvironment*>(
        contents->GetUserData(&kEnvironmentKey));
    if (!result) return nullptr;
    const auto& config = contents->GetSiteInstance()->GetStoragePartitionConfig();
    return !config.in_memory() && config.partition_domain() == kPartitionDomain &&
           config.partition_name() == result->account_id_ ? result : nullptr;
  }

  static void ConfigureNetwork(
      content::BrowserContext* browser_context,
      const content::StoragePartitionConfig& config,
      network::mojom::NetworkContextParams* params) {
    if (!params) return;
    const Entry* entry = FindEntry(browser_context, config);
    if (!entry || entry->proxy.empty()) return;
    net::ProxyConfig::ProxyRules rules;
    if (!ParseProxy(entry->proxy, &rules)) return;
    auto custom = network::mojom::CustomProxyConfig::New();
    custom->rules = std::move(rules);
    custom->should_override_existing_config = true;
    custom->allow_non_idempotent_methods = true;
    params->initial_custom_proxy_config = std::move(custom);
  }

  static void ConfigureRenderer(
      content::BrowserContext* browser_context,
      const content::StoragePartitionConfig& config,
      base::CommandLine* command_line) {
    const Entry* entry = FindEntry(browser_context, config);
    if (entry && command_line && IsValidSeed(entry->seed)) {
      command_line->AppendSwitchASCII(switches::kFingerprint, entry->seed);
    }
  }

  base::Value::Dict GetState() const {
    auto state = PersistentStateToValue(GetPersistentState());
    state.Set("url", web_contents()->GetVisibleURL().spec());
    state.Set("load_progress", web_contents()->GetLoadProgress());
    return state;
  }

  static base::Value::Dict PersistentStateToValue(const SaasAccountState& saved) {
    base::Value::Dict state;
    state.Set("id", saved.account_id);
    state.Set("account_id", saved.account_id);
    state.Set("storage_partition", saved.account_id);
    state.Set("storage_partition_persistent", true);
    state.Set("proxy_rules", saved.proxy_rules);
    state.Set("fingerprint_seed", saved.fingerprint_seed);
    return state;
  }

  SaasAccountState GetPersistentState() const {
    return {account_id_, proxy_rules_, fingerprint_seed_};
  }

 private:
  struct Entry {
    std::string proxy;
    std::string seed;
    base::WeakPtr<SaasAccountEnvironment> live;
  };
  struct Registry final : base::SupportsUserData::Data {
    std::map<std::string, Entry> entries;
  };
  static Registry& GetRegistry(content::BrowserContext* context) {
    auto* registry = static_cast<Registry*>(context->GetUserData(&kRegistryKey));
    if (!registry) {
      auto owned = std::make_unique<Registry>();
      registry = owned.get();
      context->SetUserData(&kRegistryKey, std::move(owned));
    }
    return *registry;
  }
  static const Entry* FindEntry(content::BrowserContext* context,
                               const content::StoragePartitionConfig& config) {
    if (!context || config.is_default() || config.in_memory() ||
        config.partition_domain() != kPartitionDomain) return nullptr;
    const auto* registry = static_cast<Registry*>(context->GetUserData(&kRegistryKey));
    if (!registry) return nullptr;
    auto found = registry->entries.find(config.partition_name());
    return found == registry->entries.end() ? nullptr : &found->second;
  }
  static void SetError(std::string* error, const char* value) {
    if (error) *error = value;
  }
  SaasAccountEnvironment(content::WebContents* contents, std::string account_id,
                         std::string proxy, std::string seed)
      : content::WebContentsObserver(contents), account_id_(std::move(account_id)),
        proxy_rules_(std::move(proxy)), fingerprint_seed_(std::move(seed)) {}

  inline static const char kEnvironmentKey[] = "fingerprint.android.account.environment";
  inline static const char kRegistryKey[] = "fingerprint.android.account.registry";
  const std::string account_id_, proxy_rules_, fingerprint_seed_;
  base::WeakPtrFactory<SaasAccountEnvironment> weak_factory_{this};
};

}  // namespace chrome::android

#endif  // CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_ACCOUNT_ENVIRONMENT_H_
