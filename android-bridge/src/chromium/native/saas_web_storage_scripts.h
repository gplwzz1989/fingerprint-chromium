#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_WEB_STORAGE_SCRIPTS_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_WEB_STORAGE_SCRIPTS_H_

#include <string>
#include "base/json/json_writer.h"
#include "base/values.h"

namespace chrome::android {

// 仅在 Chrome 内部隔离世界执行。普通网页不能创建或覆盖这里的文档标记。
inline std::string SaasStoragePrepareScript(const std::string& nonce) {
  auto quoted = base::WriteJson(base::Value(nonce)).value_or("null");
  return "(() => {const key='__fingerprint_saas_storage_document';"
         "if(!Object.prototype.hasOwnProperty.call(globalThis,key)){"
         "Object.defineProperty(globalThis,key,{value:" + quoted + "});}"
         "return {token:globalThis[key],url:location.href};})()";
}

inline std::string SaasStorageGuardScript(const std::string& token,
                                          const std::string& url) {
  return "if(globalThis.__fingerprint_saas_storage_document!==" +
         base::WriteJson(base::Value(token)).value_or("null") +
         "||location.href!==" + base::WriteJson(base::Value(url)).value_or("null") +
         ")return {ok:false};";
}

inline std::string SaasStorageReadScript(const std::string& token,
                                         const std::string& url,
                                         bool local = true, bool session = true) {
  return "(() => {" + SaasStorageGuardScript(token, url) +
         "const includeLocal=" + (local ? "true;" : "false;") +
         "const includeSession=" + (session ? "true;" : "false;") + R"JS(
    try {
      const encoder = new TextEncoder();
      const read = storage => {
        const result = Object.create(null);
        let bytes = 0;
        if (storage.length > 10000) throw new Error();
        for (let index = 0; index < storage.length; index++) {
          const key = storage.key(index);
          if (key === null) throw new Error();
          const value = storage.getItem(key);
          if (value === null || key.length > 1024 || value.length > 1048576) throw new Error();
          const keyBytes = encoder.encode(key).length;
          const valueBytes = encoder.encode(value).length;
          if (keyBytes > 1024 || valueBytes > 1048576) throw new Error();
          bytes += keyBytes + valueBytes;
          if (bytes > 10485760) throw new Error();
          result[key] = value;
        }
        return result;
      };
      return {ok:true,local_storage:includeLocal?read(localStorage):{},session_storage:includeSession?read(sessionStorage):{}};
    } catch (_) { return {ok:false}; }
  })())JS";
}

inline std::string SaasStorageWriteScript(const std::string& token,
                                          const std::string& url,
                                          const base::Value::Dict& payload) {
  auto json = base::WriteJson(payload);
  if (!json) return {};
  // JSON.parse 保留 "__proto__" 等真实键，不使用会改变对象原型的对象字面量。
  return "(() => {" + SaasStorageGuardScript(token, url) +
         "const input=JSON.parse(" +
         base::WriteJson(base::Value(*json)).value_or("null") + R"JS();
    const backup = storage => {
      const result = Object.create(null);
      if (storage.length > 10000) throw new Error();
      let bytes = 0;
      const encoder = new TextEncoder();
      for (let index=0; index<storage.length; index++) {
        const key=storage.key(index), value=storage.getItem(key);
        if (key===null || value===null || key.length>1024 || value.length>1048576) throw new Error();
        bytes += encoder.encode(key).length + encoder.encode(value).length;
        if (bytes>10485760) throw new Error();
        result[key]=value;
      }
      return result;
    };
    const replace = (storage, values) => {
      storage.clear();
      for (const [key,value] of Object.entries(values)) storage.setItem(key,value);
    };
    let localBefore, sessionBefore;
    try {
      if (input.local) localBefore=backup(localStorage);
      if (input.session) sessionBefore=backup(sessionStorage);
    } catch (_) { return {ok:false,rollback_failed:false}; }
    try {
      if (input.local) replace(localStorage,input.local_storage);
      if (input.session) replace(sessionStorage,input.session_storage);
      return {ok:true};
    } catch (_) {
      let failed=false;
      // 一类恢复失败仍继续尝试恢复另一类；失败状态不能被包装为成功。
      if (input.local) try { replace(localStorage,localBefore); } catch (_) { failed=true; }
      if (input.session) try { replace(sessionStorage,sessionBefore); } catch (_) { failed=true; }
      return {ok:false,rollback_failed:failed};
    }
  })())JS";
}

}  // namespace chrome::android
#endif  // CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_WEB_STORAGE_SCRIPTS_H_
