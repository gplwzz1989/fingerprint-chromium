#include <iostream>
#include <string>
#include "chrome/browser/ui/android/tab_model/saas_http_request_config.h"

// 执行实际原生参数校验函数，不代替 Chromium 网络层或 Android 设备验收。
int main() {
  using chrome::android::SaasHttpRequestConfig;
  int checks = 0;
  auto check = [&](bool result) { ++checks; if (!result) { std::cerr << "HTTP 参数测试失败：" << checks << "\n"; std::exit(1); } };
  auto input = [] { base::Value::Dict value; value.Set("url", "https://example.test/path"); return value; };
  auto accepts = [&](base::Value::Dict value) {
    SaasHttpRequestConfig result; std::string error;
    bool ok = SaasHttpRequestConfig::Parse(value, &result, &error);
    if (!ok) check(!error.empty());
    return ok;
  };
  check(accepts(input()));
  for (const char* method : {"GET", "patch", "POST", "DELETE", "OPTIONS", "PURGE", "PROPFIND"}) {
    auto value = input(); value.Set("method", method); check(accepts(std::move(value)));
  }
  for (const char* method : {"", "CONNECT", "get\r\nHeader: x", "A B", "方法"}) {
    auto value = input(); value.Set("method", method); check(!accepts(std::move(value)));
  }
  auto value = input(); value.Set("includeCredentials", true); check(!accepts(value.Clone()));
  value.Set("tabId", "account-01"); check(accepts(value.Clone()));
  value.Set("tabId", ""); check(!accepts(std::move(value)));
  value = input(); value.Set("includeCredentials", "true"); check(!accepts(std::move(value)));
  for (const char* name : {"Host", "Content-Length", "TRANSFER-ENCODING", "Connection", "Proxy-Authorization", "Upgrade"}) {
    value = input(); base::Value::Dict headers; headers.Set(name, "test"); value.Set("headers", std::move(headers)); check(!accepts(std::move(value)));
  }
  value = input(); base::Value::Dict headers; headers.Set("Cookie", "account=one"); headers.Set("Authorization", "Bearer explicit");
  value.Set("headers", headers.Clone()); check(accepts(value.Clone()));
  auto mixed = value.Clone(); mixed.Set("tabId", "account-01"); mixed.Set("includeCredentials", true); check(!accepts(std::move(mixed)));
  headers.Set("authorization", "Bearer duplicate"); value.Set("headers", std::move(headers)); check(!accepts(std::move(value)));
  for (const std::string& field : {std::string("x\r\nCookie: x"), std::string("x\0y", 3), std::string(8193, 'x')}) {
    value = input(); base::Value::Dict fields; fields.Set("X-Custom", field); value.Set("headers", std::move(fields)); check(!accepts(std::move(value)));
  }
  value = input(); value.Set("unknown", true); check(!accepts(std::move(value)));
  value = input(); value.Set("url", "https://example.test/\n"); check(!accepts(std::move(value)));
  value = input(); value.Set("url", std::string(8193, 'a')); check(!accepts(std::move(value)));
  for (const char* encoded : {"YQ", "YR==", "YQ==\n", "===="}) {
    value = input(); value.Set("method", "POST"); value.Set("bodyBase64", encoded); check(!accepts(std::move(value)));
  }
  value = input(); value.Set("method", "POST"); value.Set("bodyBase64", "AAH/");
  SaasHttpRequestConfig config; std::string error;
  check(SaasHttpRequestConfig::Parse(value, &config, &error)); check(config.body == std::string("\0\1\xff", 3));
  value.Set("bodyBase64", ""); check(accepts(value.Clone()));
  value.Set("method", "HEAD"); value.Set("bodyBase64", "YQ=="); check(!accepts(std::move(value)));
  for (const char* method : {"GET", "HEAD"}) {
    value = input(); value.Set("method", method); value.Set("bodyBase64", ""); check(!accepts(std::move(value)));
  }
  value = input(); value.Set("bodyBase64", std::string(((SaasHttpRequestConfig::kMaxRequestBytes + 2) / 3) * 4 + 4, 'A')); check(!accepts(std::move(value)));
  value = input(); value.Set("method", "POST"); value.Set("bodyBase64", base::Base64Encode(std::string(SaasHttpRequestConfig::kMaxRequestBytes, 'x'))); check(accepts(std::move(value)));
  value = input(); value.Set("contentType", "text/plain"); base::Value::Dict mime; mime.Set("Content-Type", "application/json"); value.Set("headers", std::move(mime)); check(!accepts(std::move(value)));
  check(SaasHttpRequestConfig::Token("X-Test")); check(!SaasHttpRequestConfig::Token("X Test"));
  std::cout << "原生 HTTP 参数测试通过：" << checks << " 项\n";
}
