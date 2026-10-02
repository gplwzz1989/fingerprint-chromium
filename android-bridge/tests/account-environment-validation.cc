// 仅测试实际原生校验函数；不使用模拟环境代替真实 StoragePartition。
#include "chrome/browser/ui/android/tab_model/saas_account_environment.h"
#include <cstdio>
#include <vector>

int main() {
  using Environment = chrome::android::SaasAccountEnvironment;
  int checks = 0, failures = 0;
  const auto verify = [&](bool actual, bool expected) {
    ++checks;
    if (actual != expected) { ++failures; std::fprintf(stderr, "原生参数校验结果不一致\n"); }
  };
  for (const auto& value : std::vector<std::string>{"account-01", "A_B_2", std::string(128, 'a')}) verify(Environment::IsValidAccountId(value), true);
  for (const auto& value : std::vector<std::string>{"", "账号", "../account", "a b", std::string(129, 'a'), std::string("a\0b", 3)}) verify(Environment::IsValidAccountId(value), false);
  for (const std::string value : {"0", "1", "4294967295"}) verify(Environment::IsValidSeed(value), true);
  for (const auto& value : std::vector<std::string>{"", "+1", "-1", " 1", "1.0", "4294967296", std::string("1\0", 2)}) verify(Environment::IsValidSeed(value), false);
  for (const std::string value : {"", "http://127.0.0.1:8888", "socks5://127.0.0.1:1080"}) {
    net::ProxyConfig::ProxyRules rules; verify(Environment::ParseProxy(value, &rules), true);
  }
  for (const auto& value : std::vector<std::string>{"http://user:secret@127.0.0.1:80", "http=127.0.0.1:80", "a:80;b:81", "a:80,b:81", "a:80\n", std::string("a:80\0", 5), std::string(2049, 'a')}) {
    net::ProxyConfig::ProxyRules rules; verify(Environment::ParseProxy(value, &rules), false);
  }
  std::printf("原生账号参数校验通过：%d 项\n", checks);
  return failures ? 1 : 0;
}
