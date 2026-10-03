#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_HTTP_REQUEST_CONFIG_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_HTTP_REQUEST_CONFIG_H_

#include <map>
#include <optional>
#include <string>
#include <string_view>
#include "base/base64.h"
#include "base/strings/string_util.h"
#include "base/values.h"

namespace chrome::android {

// 平台无关的网络输入校验；地址的实际 HTTP/HTTPS 解析由原生网络宿主再校验。
struct SaasHttpRequestConfig {
  static constexpr size_t kMaxRequestBytes = 8 * 1024 * 1024;
  static constexpr size_t kMaxResponseBytes = 10 * 1024 * 1024;
  std::string url;
  std::string method = "GET";
  std::map<std::string, std::string> headers;
  std::string body;
  std::string content_type = "application/octet-stream";
  bool has_body = false;
  bool include_credentials = false;

  static bool Token(std::string_view text) {
    if (text.empty()) return false;
    for (unsigned char c : text) {
      if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
            (c >= '0' && c <= '9') || std::string_view("!#$%&'*+-.^_`|~").find(c) != std::string_view::npos)) return false;
    }
    return true;
  }
  static bool HeaderValue(std::string_view text) {
    for (unsigned char c : text) if ((c < 32 && c != '\t') || c == 127) return false;
    return true;
  }
  static bool Parse(const base::Value::Dict& input, SaasHttpRequestConfig* output, std::string* error) {
    auto reject = [&](const char* message) { *error = message; return false; };
    SaasHttpRequestConfig value;
    for (const auto [name, field] : input) {
      if (name != "url" && name != "method" && name != "headers" && name != "bodyBase64" &&
          name != "contentType" && name != "includeCredentials" && name != "tabId") return reject("HTTP 请求包含不支持的字段");
    }
    const auto* url = input.FindString("url");
    if (!url || url->empty() || url->size() > 8192 || !HeaderValue(*url) || url->find('\t') != std::string::npos) return reject("HTTP 地址无效或过长");
    value.url = *url;
    if (const auto* method = input.Find("method")) {
      if (!method->is_string() || method->GetString().size() > 32 || !Token(method->GetString())) return reject("HTTP 方法无效");
      value.method = base::ToUpperASCII(method->GetString());
    }
    if (value.method == "CONNECT") return reject("原生 HTTP 不支持隧道连接方法");
    if (const auto* credentials = input.Find("includeCredentials")) {
      auto flag = credentials->GetIfBool();
      if (!flag) return reject("是否携带账号凭据必须为布尔值");
      value.include_credentials = *flag;
    }
    if (const auto* id = input.Find("tabId")) {
      if (!id->is_string() || id->GetString().empty() || id->GetString().size() > 128) return reject("HTTP 账号标签标识无效");
    }
    if (value.include_credentials && !input.FindString("tabId")) return reject("自动携带凭据时必须指定隔离账号标签");
    size_t header_bytes = 0;
    if (const auto* fields = input.Find("headers")) {
      const auto* headers = fields->GetIfDict();
      if (!headers || headers->size() > 100) return reject("HTTP 请求头过多或格式无效");
      for (const auto [name, field] : *headers) {
        if (name.size() > 128 || !Token(name) || !field.is_string() || field.GetString().size() > 8192 ||
            !HeaderValue(field.GetString())) return reject("HTTP 请求头无效");
        std::string lower = base::ToLowerASCII(name);
        for (const char* reserved : {"host", "content-length", "transfer-encoding", "connection", "proxy-connection",
                                    "proxy-authorization", "keep-alive", "te", "trailer", "upgrade"}) {
          if (lower == reserved) return reject("传输连接和正文长度请求头由原生网络层管理");
        }
        header_bytes += name.size() + field.GetString().size();
        if (header_bytes > 32768 || value.headers.contains(lower)) return reject("HTTP 请求头重复或超过大小限制");
        value.headers.emplace(lower, field.GetString());
      }
    }
    if (value.include_credentials && value.headers.contains("cookie")) return reject("自动账号凭据与显式 Cookie 请求头不能同时使用");
    if (const auto* type = input.Find("contentType")) {
      if (!type->is_string() || type->GetString().empty() || type->GetString().size() > 256 || !HeaderValue(type->GetString())) return reject("HTTP 内容类型无效");
      value.content_type = type->GetString();
      auto header = value.headers.find("content-type");
      if (header != value.headers.end() && header->second != value.content_type) return reject("HTTP 内容类型与请求头不一致");
    } else if (auto header = value.headers.find("content-type"); header != value.headers.end()) {
      value.content_type = header->second;
    }
    if (const auto* body = input.Find("bodyBase64")) {
      if (!body->is_string() || body->GetString().size() > ((kMaxRequestBytes + 2) / 3) * 4 ||
          !base::Base64Decode(body->GetString(), &value.body) || value.body.size() > kMaxRequestBytes ||
          base::Base64Encode(value.body) != body->GetString()) return reject("HTTP 正文编码无效或超过 8 MiB 限制");
      value.has_body = true;
      if (value.method == "GET" || value.method == "HEAD") return reject("GET 和 HEAD 不能指定上传正文");
      if (!value.body.empty() && value.method == "TRACE") return reject("TRACE 不能携带非空正文");
    }
    *output = std::move(value);
    return true;
  }
};

}  // namespace chrome::android
#endif
