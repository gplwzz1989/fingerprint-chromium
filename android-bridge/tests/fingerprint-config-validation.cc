// 实际配置解析和 UA 元数据测试，不使用模拟 WebContents 代替 Android 页面验证。
#include "chrome/browser/ui/android/tab_model/saas_fingerprint_config.h"
#if !defined(FINGERPRINT_CONFIG_ONLY)
#include "chrome/browser/ui/android/tab_model/saas_user_agent_metadata.h"
#endif
#include <cstdio>
#include <vector>

int main() {
  using namespace chrome::android;
  int checks = 0, failures = 0;
  const auto verify = [&](bool result) { ++checks; if (!result) ++failures; };
  SaasFingerprintConfig current{"Mozilla/5.0 Chrome/144.0.0.0", 7}, output;
  std::string error;
  auto patch = [](const char* key, base::Value value) {
    base::Value::Dict input; input.Set(key, std::move(value)); return input;
  };
  for (int value : {0, 1, 7, 64}) {
    verify(SaasFingerprintConfig::Update(patch("hardware_concurrency", base::Value(value)), current, &output, &error));
    verify(output.hardware_concurrency == value && output.user_agent == current.user_agent);
  }
  for (int value : {-1, 65}) verify(!SaasFingerprintConfig::Update(patch("hardware_concurrency", base::Value(value)), current, &output, &error));
  verify(!SaasFingerprintConfig::Update(patch("hardware_concurrency", base::Value(2.5)), current, &output, &error));
  verify(!SaasFingerprintConfig::Update(patch("hardware_concurrency", base::Value(true)), current, &output, &error));
  verify(!SaasFingerprintConfig::Update(patch("hardware_concurrency", base::Value("7")), current, &output, &error));
  for (const auto& value : std::vector<std::string>{"", "CustomAgent/1", std::string(512, 'A')}) {
    verify(SaasFingerprintConfig::Update(patch("user_agent", base::Value(value)), current, &output, &error));
    verify(output.user_agent == value && output.hardware_concurrency == 7);
  }
  for (const auto& value : std::vector<std::string>{"bad\nUA", "bad\rUA", "bad\tUA", "中文UA",
           std::string("A\0B", 3), std::string(513, 'A')}) {
    output = current;
    verify(!SaasFingerprintConfig::Update(patch("user_agent", base::Value(value)), current, &output, &error));
    verify(output.user_agent == current.user_agent && output.hardware_concurrency == 7);
  }
  verify(!SaasFingerprintConfig::Update({}, current, &output, &error));
  verify(!SaasFingerprintConfig::Update(patch("fingerprint_seed", base::Value("2")), current, &output, &error));
  auto invalid = patch("user_agent", base::Value("NewAgent/1"));
  invalid.Set("hardware_concurrency", 999);
  output = current;
  verify(!SaasFingerprintConfig::Update(invalid, current, &output, &error));
  verify(output.user_agent == current.user_agent && output.hardware_concurrency == 7);
  auto reset = patch("user_agent", base::Value(""));
  reset.Set("hardware_concurrency", 0);
  verify(SaasFingerprintConfig::Update(reset, current, &output, &error));
  verify(output.user_agent.empty() && output.hardware_concurrency == 0);
#if !defined(FINGERPRINT_CONFIG_ONLY)
  auto phone = SaasUserAgentMetadata("Mozilla/5.0 (Linux; Android 13; Pixel 7 Build/TQ3A) AppleWebKit/537.36 Chrome/144.0.0.0 Mobile Safari/537.36");
  verify(phone && phone->platform == "Android" && phone->mobile && phone->model == "Pixel 7");
  verify(phone && phone->platform_version == "13" && phone->architecture.empty() && phone->bitness.empty());
  verify(phone && phone->full_version == "144.0.0.0" && phone->form_factors.front() == "Mobile");
  auto tablet = SaasUserAgentMetadata("Mozilla/5.0 (Linux; Android 13; TabletModel) Chrome/144.0.0.0 Safari/537.36");
  verify(tablet && !tablet->mobile && tablet->form_factors.front() == "Tablet" && tablet->model == "TabletModel");
  auto desktop = SaasUserAgentMetadata("Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/144.0.0.0 Edg/144.0.9.0");
  verify(desktop && desktop->architecture == "x86" && desktop->bitness == "64");
  verify(desktop && desktop->brand_full_version_list.back().brand == "Microsoft Edge" &&
         desktop->brand_full_version_list.back().version == "144.0.9.0");
  auto mac = SaasUserAgentMetadata("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/144.0.0.0");
  verify(mac && mac->platform_version == "10.15.7");
  for (const std::string value : {"CustomAgent/1", "Chrome/..", "Chrome/abc", "Chrome/1..2"}) verify(!SaasUserAgentMetadata(value));
#endif
#if defined(FINGERPRINT_CONFIG_ONLY)
  std::printf("原生指纹配置校验：%d 项，失败 %d 项；未运行 UA 元数据测试\n", checks, failures);
#else
  std::printf("原生指纹配置与元数据校验：%d 项，失败 %d 项\n", checks, failures);
#endif
  return failures ? 1 : 0;
}
