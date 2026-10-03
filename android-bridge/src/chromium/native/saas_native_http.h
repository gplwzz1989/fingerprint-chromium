#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_NATIVE_HTTP_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_NATIVE_HTTP_H_

#include <memory>
#include <optional>
#include <string>
#include "base/functional/bind.h"
#include "base/supports_user_data.h"
#include "base/time/time.h"
#include "base/timer/timer.h"
#include "chrome/browser/profiles/profile.h"
#include "chrome/browser/ui/android/tab_model/saas_account_environment.h"
#include "chrome/browser/ui/android/tab_model/saas_http_request_config.h"
#include "content/public/browser/render_frame_host.h"
#include "content/public/browser/navigation_handle.h"
#include "content/public/browser/storage_partition.h"
#include "content/public/browser/weak_document_ptr.h"
#include "net/base/load_flags.h"
#include "net/base/net_errors.h"
#include "net/cookies/site_for_cookies.h"
#include "net/http/http_response_headers.h"
#include "net/url_request/redirect_info.h"
#include "net/traffic_annotation/network_traffic_annotation.h"
#include "services/network/public/cpp/resource_request.h"
#include "services/network/public/cpp/shared_url_loader_factory.h"
#include "services/network/public/cpp/simple_url_loader.h"
#include "services/network/public/mojom/url_response_head.mojom.h"
#include "url/origin.h"

namespace chrome::android {

// 只提供底层网络能力；默认使用无持久化的独立分区，绝不使用 Profile 共享 Cookie。
class SaasNativeHttp final : public content::WebContentsObserver,
                             public std::enable_shared_from_this<SaasNativeHttp> {
 public:
  using Authority = base::RepeatingCallback<bool()>;
  using Completion = base::OnceCallback<void(base::Value::Dict)>;
  static void Start(Profile* profile, content::WebContents* account,
                    const base::Value::Dict& input, Authority authority, Completion completion) {
    auto reject = [&](const std::string& message) {
      base::Value::Dict reply; reply.Set("ok", false); reply.Set("error", message);
      std::move(completion).Run(std::move(reply));
    };
    SaasHttpRequestConfig config;
    std::string error;
    if (!SaasHttpRequestConfig::Parse(input, &config, &error)) { reject(error); return; }
    if (!profile || profile->IsOffTheRecord() || authority.is_null() || !authority.Run()) { reject("页面授权或网络环境未就绪"); return; }
    GURL url(config.url);
    if (!url.is_valid() || !url.SchemeIsHTTPOrHTTPS() || url.has_username() || url.has_password()) { reject("仅支持不含用户名密码的 HTTP 或 HTTPS 地址"); return; }
    auto* environment = SaasAccountEnvironment::FromWebContents(account);
    if (input.contains("tabId") && (!environment || account->GetBrowserContext() != profile)) { reject("隔离账号网络环境不存在"); return; }
    if (config.include_credentials && !environment) { reject("自动携带凭据必须使用指定账号的独立分区"); return; }
    if (environment && !environment->GetPersistentState().user_agent.empty()) {
      config.headers.try_emplace("user-agent", environment->GetPersistentState().user_agent);
    }
    auto* partition = environment ? profile->GetStoragePartition(account->GetSiteInstance()) :
        profile->GetStoragePartition(content::StoragePartitionConfig::Create(profile, "saasandroidhttp", "stateless", true));
    if (!partition) { reject("原生网络分区不可用"); return; }
    auto* data = static_cast<BudgetData*>(profile->GetUserData(&kBudgetKey));
    if (!data) { auto owned = std::make_unique<BudgetData>(); data = owned.get(); profile->SetUserData(&kBudgetKey, std::move(owned)); }
    if (data->budget->active >= 4) { reject("HTTP 请求数量已达上限，请稍后重试"); return; }
    auto operation = std::shared_ptr<SaasNativeHttp>(new SaasNativeHttp(account,
        partition->GetURLLoaderFactoryForBrowserProcess(), std::move(authority), std::move(completion), data->budget));
    operation->keep_alive_ = operation;
    operation->original_origin_ = url::Origin::Create(url);
    operation->Begin(std::move(config), url);
  }
  ~SaasNativeHttp() override { --budget_->active; }

 private:
  struct Budget { int active = 0; };
  struct BudgetData final : base::SupportsUserData::Data { std::shared_ptr<Budget> budget = std::make_shared<Budget>(); };
  inline static const char kBudgetKey = 0;
  SaasNativeHttp(content::WebContents* account, scoped_refptr<network::SharedURLLoaderFactory> factory,
                 Authority authority, Completion completion, std::shared_ptr<Budget> budget)
      : content::WebContentsObserver(account), factory_(std::move(factory)),
        authority_(std::move(authority)), completion_(std::move(completion)), budget_(std::move(budget)),
        needs_account_(account != nullptr) {
    ++budget_->active;
    if (account && account->GetPrimaryMainFrame()) document_ = account->GetPrimaryMainFrame()->GetWeakDocumentPtr();
  }
  bool Check() {
    if (!finished_ && base::TimeTicks::Now() >= deadline_) { Fail("HTTP 请求超时，请重试"); return false; }
    auto* frame = document_.AsRenderFrameHostIfValid();
    if (!finished_ && authority_.Run() && (!needs_account_ || (web_contents() && frame && frame->IsActive() &&
        web_contents()->GetPrimaryMainFrame() == frame && SaasAccountEnvironment::FromWebContents(web_contents())))) return true;
    Fail("页面或账号授权已失效，HTTP 请求已停止"); return false;
  }
  void Begin(SaasHttpRequestConfig config, const GURL& url) {
    if (!Check()) return;
    auto request = std::make_unique<network::ResourceRequest>();
    request->url = url; request->method = config.method;
    request->credentials_mode = config.include_credentials ? network::mojom::CredentialsMode::kInclude : network::mojom::CredentialsMode::kOmit;
    request->site_for_cookies = config.include_credentials ? net::SiteForCookies::FromUrl(url) : net::SiteForCookies();
    request->load_flags = net::LOAD_DISABLE_CACHE | net::LOAD_DO_NOT_USE_EMBEDDED_IDENTITY;
    request->do_not_prompt_for_login = true;
    if (!config.include_credentials) request->load_flags |= net::LOAD_DO_NOT_SAVE_COOKIES;
    for (const auto& [name, value] : config.headers) request->headers.SetHeader(name, value);
    constexpr auto annotation = net::DefineNetworkTrafficAnnotation("fingerprint_saas_android_http", R"(
      semantics {
        sender: "Fingerprint Android native bridge"
        description: "An explicitly allowlisted SaaS origin requests native HTTP access."
        trigger: "The SaaS page invokes http.request."
        data: "Explicit URL, headers and request body."
        destination: OTHER
      }
      policy {
        cookies_allowed: YES
        cookies_store: "Only the explicitly selected isolated account partition."
        setting: "Only compile-time allowlisted SaaS origins can invoke the API."
        policy_exception_justification: "Native HTTP access is a required product capability."
      })");
    loader_ = network::SimpleURLLoader::Create(std::move(request), annotation);
    loader_->SetAllowHttpErrorResults(true);
    loader_->SetTimeoutDuration(base::Seconds(30));
    if (config.has_body) loader_->AttachStringForUpload(std::move(config.body), config.content_type);
    std::weak_ptr<SaasNativeHttp> weak = shared_from_this();
    loader_->SetOnRedirectCallback(base::BindRepeating([](std::weak_ptr<SaasNativeHttp> weak,
        const GURL&, const net::RedirectInfo& redirect, const network::mojom::URLResponseHead&,
        std::vector<std::string>*) {
      if (auto current = weak.lock()) {
        if (!current->Check()) return;
        if (++current->redirects_ > 5 || !redirect.new_url.SchemeIsHTTPOrHTTPS() ||
            redirect.new_url.has_username() || redirect.new_url.has_password() ||
            url::Origin::Create(redirect.new_url) != current->original_origin_) {
          current->Fail("HTTP 重定向超限或跨来源，请直接请求目标地址");
        }
      }
    }, weak));
    poll_.Start(FROM_HERE, base::Milliseconds(100), base::BindRepeating([](std::weak_ptr<SaasNativeHttp> weak) {
      if (auto current = weak.lock()) current->Check();
    }, weak));
    timeout_.Start(FROM_HERE, base::Seconds(30), base::BindOnce([](std::weak_ptr<SaasNativeHttp> weak) {
      if (auto current = weak.lock()) current->Fail("HTTP 请求超时，请重试");
    }, weak));
    loader_->DownloadToString(factory_.get(), base::BindOnce([](std::weak_ptr<SaasNativeHttp> weak, std::optional<std::string> body) {
      if (auto current = weak.lock()) current->Complete(std::move(body));
    }, weak), SaasHttpRequestConfig::kMaxResponseBytes);
  }
  void Complete(std::optional<std::string> body) {
    if (!Check()) return;
    if (!body || loader_->NetError() != net::OK) { Fail("HTTP 请求失败或响应超过 10 MiB 限制"); return; }
    const auto* info = loader_->ResponseInfo();
    if (!info || !info->headers) { Fail("HTTP 响应信息不完整"); return; }
    if (info->headers->response_code() < 100 || info->headers->response_code() > 599) { Fail("HTTP 响应状态码无效"); return; }
    base::Value::Dict headers;
    size_t iterator = 0, bytes = 0;
    int count = 0;
    std::string name, value;
    while (info->headers->EnumerateHeaderLines(&iterator, &name, &value)) {
      name = base::ToLowerASCII(name);
      bytes += name.size() + value.size();
      if (++count > 100 || bytes > 32768 || name.size() > 128 || !SaasHttpRequestConfig::Token(name) ||
          value.size() > 8192 || !SaasHttpRequestConfig::HeaderValue(value) || !base::IsStringUTF8(value)) { Fail("HTTP 响应头格式无效或超过支持范围"); return; }
      // 重复头不能被静默覆盖；契约补充 headersList 保留每个真实字段值。
      auto* existing = headers.FindString(name);
      headers.Set(name, existing ? *existing + "\n" + value : value);
    }
    base::Value::List header_list;
    iterator = 0;
    while (info->headers->EnumerateHeaderLines(&iterator, &name, &value)) {
      base::Value::Dict entry; entry.Set("name", base::ToLowerASCII(name)); entry.Set("value", value); header_list.Append(std::move(entry));
    }
    base::Value::Dict result;
    result.Set("status", info->headers->response_code()); result.Set("headers", std::move(headers));
    result.Set("headersList", std::move(header_list)); result.Set("bodyBase64", base::Base64Encode(*body));
    base::Value::Dict reply; reply.Set("ok", true); reply.Set("result", std::move(result));
    Finish(std::move(reply));
  }
  void Fail(const std::string& message) {
    if (finished_) return;
    base::Value::Dict reply; reply.Set("ok", false); reply.Set("error", message); Finish(std::move(reply));
  }
  void Finish(base::Value::Dict reply) {
    if (finished_) return;
    finished_ = true; poll_.Stop(); timeout_.Stop(); loader_.reset(); factory_.reset(); Observe(nullptr);
    std::move(completion_).Run(std::move(reply));
    keep_alive_.reset();
  }
  void WebContentsDestroyed() override { Fail("账号标签已关闭，HTTP 请求已停止"); }
  void DidStartNavigation(content::NavigationHandle* navigation) override {
    if (navigation->IsInPrimaryMainFrame() && !navigation->IsSameDocument()) Fail("账号页面正在跳转，HTTP 请求已停止");
  }
  scoped_refptr<network::SharedURLLoaderFactory> factory_;
  Authority authority_;
  Completion completion_;
  std::shared_ptr<Budget> budget_;
  std::shared_ptr<SaasNativeHttp> keep_alive_;
  content::WeakDocumentPtr document_;
  url::Origin original_origin_;
  bool needs_account_ = false, finished_ = false;
  int redirects_ = 0;
  const base::TimeTicks deadline_ = base::TimeTicks::Now() + base::Seconds(30);
  base::RepeatingTimer poll_;
  base::OneShotTimer timeout_;
  std::unique_ptr<network::SimpleURLLoader> loader_;
};
}  // namespace chrome::android
#endif
