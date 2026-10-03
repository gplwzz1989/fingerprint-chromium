// 仅输出实际原生脚本供边界测试使用，不读取用户账号或模拟 Android 存储接口。
#include "chrome/browser/ui/android/tab_model/saas_web_storage_scripts.h"
#include <cstdio>

int main() {
  using namespace chrome::android;
  const std::string token = "11111111-2222-4333-8444-555555555555";
  const std::string url = "https://storage.example.test/account";
  base::Value::Dict local;
  local.Set("__proto__", "真实键");
  local.Set("constructor", "保留键");
  local.Set("测试", "中文与换行\n内容");
  base::Value::Dict session;
  session.Set("session", "新会话");
  base::Value::Dict payload;
  payload.Set("local", true); payload.Set("session", true);
  payload.Set("local_storage", local.Clone()); payload.Set("session_storage", session.Clone());
  base::Value::Dict scripts;
  scripts.Set("prepare", SaasStoragePrepareScript(token));
  scripts.Set("read", SaasStorageReadScript(token, url));
  scripts.Set("readNeither", SaasStorageReadScript(token, url, false, false));
  scripts.Set("write", SaasStorageWriteScript(token, url, payload));
  payload.Set("session", false);
  scripts.Set("localOnly", SaasStorageWriteScript(token, url, payload));
  payload.Set("local", false);
  scripts.Set("neither", SaasStorageWriteScript(token, url, payload));
  auto json = base::WriteJson(scripts);
  if (!json) return 1;
  std::fwrite(json->data(), 1, json->size(), stdout);
  return 0;
}
