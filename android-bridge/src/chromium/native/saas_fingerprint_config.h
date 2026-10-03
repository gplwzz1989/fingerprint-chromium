#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_FINGERPRINT_CONFIG_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_FINGERPRINT_CONFIG_H_

#include <string>
#include <string_view>
#include <utility>
#include "base/values.h"

namespace chrome::android {

struct SaasFingerprintConfig {
  std::string user_agent;
  int hardware_concurrency = 0;

  static bool IsValidUserAgent(std::string_view value) {
    if (value.size() > 512) return false;
    for (unsigned char c : value) if (c < 0x20 || c > 0x7e) return false;
    return true;
  }

  bool IsValid() const {
    return IsValidUserAgent(user_agent) && hardware_concurrency >= 0 &&
           hardware_concurrency <= 64;
  }

  base::Value::Dict ToValue() const {
    base::Value::Dict value;
    value.Set("user_agent", user_agent);
    value.Set("hardware_concurrency", hardware_concurrency);
    return value;
  }

  // 先完整校验再修改；空 UA 和 0 分别表示清除自定义 UA、恢复种子驱动的页面硬件值。
  static bool Update(const base::Value::Dict& input, const SaasFingerprintConfig& current,
                     SaasFingerprintConfig* output, std::string* error) {
    const auto fail = [&](const char* message) { if (error) *error = message; return false; };
    if (input.empty()) return fail("请提供需要修改的指纹配置");
    SaasFingerprintConfig next = current;
    for (auto entry : input) {
      if (entry.first == "user_agent") {
        if (!entry.second.is_string() || !IsValidUserAgent(entry.second.GetString())) {
          return fail("User-Agent 必须是 512 字节以内、不含控制字符的英文请求头");
        }
        next.user_agent = entry.second.GetString();
      } else if (entry.first == "hardware_concurrency") {
        if (!entry.second.is_int() || entry.second.GetInt() < 0 || entry.second.GetInt() > 64) {
          return fail("硬件并发数必须是 0 到 64 的整数");
        }
        next.hardware_concurrency = entry.second.GetInt();
      } else {
        return fail("包含不支持的指纹字段；指纹种子和代理请在创建账号环境时设置");
      }
    }
    if (!next.IsValid()) return fail("指纹配置无效");
    *output = std::move(next);
    return true;
  }
};

}  // namespace chrome::android
#endif  // CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_FINGERPRINT_CONFIG_H_
