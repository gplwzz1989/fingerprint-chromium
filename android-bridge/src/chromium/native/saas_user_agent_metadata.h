#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_USER_AGENT_METADATA_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_USER_AGENT_METADATA_H_

#include <optional>
#include <algorithm>
#include <string>
#include <string_view>
#include <utility>
#include "third_party/blink/public/common/user_agent/user_agent_metadata.h"

namespace chrome::android {

inline std::optional<std::string> SaasUserAgentToken(
    const std::string& agent, std::string_view token) {
  size_t start = agent.find(token);
  if (start == std::string::npos) return std::nullopt;
  start += token.size();
  size_t end = agent.find_first_of(" ;)", start);
  std::string value = agent.substr(start, end == std::string::npos ? end : end - start);
  return value.empty() ? std::nullopt : std::make_optional(std::move(value));
}

inline bool SaasUserAgentVersion(std::string_view value) {
  if (value.empty() || value.size() > 32 || value.front() == '.' || value.back() == '.') return false;
  bool dot = false;
  for (unsigned char c : value) {
    if (c == '.') { if (dot) return false; dot = true; }
    else { if (c < '0' || c > '9') return false; dot = false; }
  }
  return true;
}

// 元数据仅从配置字符串提取；未知架构/型号留空，不把本机真实设备信息混入自定义 UA。
// 非 Chromium UA 无可确认的 CH 元数据，调用方使用 UA-only 覆盖以抑制默认 CH 泄露。
inline std::optional<blink::UserAgentMetadata> SaasUserAgentMetadata(const std::string& agent) {
  auto chrome = SaasUserAgentToken(agent, "Chrome/");
  if (!chrome || !SaasUserAgentVersion(*chrome)) return std::nullopt;
  blink::UserAgentMetadata meta;
  meta.full_version = *chrome;
  std::string major = chrome->substr(0, chrome->find('.'));
  meta.brand_version_list.emplace_back("Chromium", major);
  meta.brand_full_version_list.emplace_back("Chromium", *chrome);
  const char* brand = "Google Chrome";
  std::string version = *chrome;
  for (const auto& product : {std::pair{"Edg/", "Microsoft Edge"}, std::pair{"OPR/", "Opera"},
                              std::pair{"Vivaldi/", "Vivaldi"}}) {
    if (auto token = SaasUserAgentToken(agent, product.first); token && SaasUserAgentVersion(*token)) {
      brand = product.second; version = *token; break;
    }
  }
  meta.brand_version_list.emplace_back(brand, version.substr(0, version.find('.')));
  meta.brand_full_version_list.emplace_back(brand, version);
  meta.mobile = agent.find("Mobile") != std::string::npos;
  if (agent.find("Android") != std::string::npos) {
    meta.platform = "Android";
    meta.platform_version = SaasUserAgentToken(agent, "Android ").value_or("");
    size_t start = agent.find("Android ");
    size_t model = agent.find(';', start);
    size_t end = agent.find(')', start);
    if (model != std::string::npos && end != std::string::npos && model < end) {
      size_t build = agent.find(" Build/", model);
      size_t limit = std::min(end, build);
      size_t last_separator = agent.rfind(';', limit);
      std::string name = agent.substr(last_separator + 1, limit - last_separator - 1);
      size_t first = name.find_first_not_of(' '), last = name.find_last_not_of(' ');
      // 多段且没有 Build 标记时无法确认设备字段，不能把语言或 WebView 标记当型号。
      if (first != std::string::npos && (build < end || last_separator == model)) {
        meta.model = name.substr(first, last - first + 1);
      }
    }
  } else if (agent.find("Windows NT ") != std::string::npos) {
    meta.platform = "Windows"; meta.platform_version = SaasUserAgentToken(agent, "Windows NT ").value_or("");
  } else if (agent.find("Mac OS X ") != std::string::npos) {
    meta.platform = "macOS"; meta.platform_version = SaasUserAgentToken(agent, "Mac OS X ").value_or("");
    for (char& c : meta.platform_version) if (c == '_') c = '.';
  } else if (agent.find("Linux") != std::string::npos) {
    meta.platform = "Linux";
  }
  if (agent.find("arm64") != std::string::npos || agent.find("aarch64") != std::string::npos) {
    meta.architecture = "arm"; meta.bitness = "64";
  } else if (agent.find("Win64") != std::string::npos || agent.find("x86_64") != std::string::npos ||
             agent.find("x64") != std::string::npos) {
    meta.architecture = "x86"; meta.bitness = "64";
  } else if (agent.find("i686") != std::string::npos || agent.find("x86;") != std::string::npos) {
    meta.architecture = "x86"; meta.bitness = "32";
  }
  meta.form_factors.push_back(meta.mobile ? blink::kMobileFormFactor :
      meta.platform == "Android" ? blink::kTabletFormFactor : blink::kDesktopFormFactor);
  return meta;
}

}  // namespace chrome::android
#endif  // CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_USER_AGENT_METADATA_H_
