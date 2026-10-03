#ifndef CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_ACCOUNT_STATE_H_
#define CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_ACCOUNT_STATE_H_

#include <cstdint>
#include <optional>
#include <string>
#include <string_view>
#include <utility>

#include "base/pickle.h"
#include "chrome/browser/ui/android/tab_model/saas_fingerprint_config.h"

namespace chrome::android {

// 分区身份是必需的顶层字段，不放入可能被导航大小预算截断的 extended_info。
// 零条导航前缀让旧解析器受控拒绝，避免负数数量触发巨量内存申请。
struct SaasAccountState {
  static constexpr int kMarker = -0x53414153;
  static constexpr int kVersion = 2;
  static constexpr int kMaxEntries = 10000;

  std::string account_id;
  std::string proxy_rules;
  std::string fingerprint_seed;
  std::string user_agent = {};
  int hardware_concurrency = 0;

  static bool IsValidAccountId(std::string_view value) {
    if (value.empty() || value.size() > 128) return false;
    for (unsigned char c : value) {
      if (!((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
            (c >= '0' && c <= '9') || c == '-' || c == '_')) return false;
    }
    return true;
  }

  static bool IsValidSeed(std::string_view value) {
    if (value.empty() || value.size() > 10) return false;
    uint64_t seed = 0;
    for (unsigned char c : value) {
      if (c < '0' || c > '9') return false;
      seed = seed * 10 + (c - '0');
    }
    return seed <= 0xffffffffULL;
  }

  bool IsValid() const {
    return IsValidAccountId(account_id) && IsValidSeed(fingerprint_seed) &&
           SaasFingerprintConfig{user_agent, hardware_concurrency}.IsValid() &&
           proxy_rules.size() <= 2048 &&
           proxy_rules.find_first_of(";,=@\r\n\0", 0, 7) == std::string::npos;
  }

  static bool WriteHeader(base::Pickle* pickle, bool off_the_record,
                          int entry_count, int current_entry_index,
                          const std::optional<SaasAccountState>& state) {
    if (!pickle || entry_count < 1 || entry_count > kMaxEntries ||
        current_entry_index < 0 || current_entry_index >= entry_count ||
        (state && (off_the_record || !state->IsValid()))) return false;
    pickle->WriteBool(off_the_record);
    if (state) {
      pickle->WriteInt(0);
      pickle->WriteInt(kMarker);
      pickle->WriteInt(kVersion);
      pickle->WriteString(state->account_id);
      pickle->WriteString(state->proxy_rules);
      pickle->WriteString(state->fingerprint_seed);
      pickle->WriteString(state->user_agent);
      pickle->WriteInt(state->hardware_concurrency);
    }
    pickle->WriteInt(entry_count);
    pickle->WriteInt(current_entry_index);
    return true;
  }

  static bool ReadHeader(base::PickleIterator* iter, bool* off_the_record,
                         int* entry_count, int* current_entry_index,
                         std::optional<SaasAccountState>* state) {
    state->reset();
    int count;
    if (!iter->ReadBool(off_the_record) || !iter->ReadInt(&count)) return false;
    std::optional<SaasAccountState> parsed;
    if (count == 0) {
      int marker, version;
      std::string_view account, proxy, seed;
      if (*off_the_record || !iter->ReadInt(&marker) || marker != kMarker ||
          !iter->ReadInt(&version) || (version != 1 && version != kVersion) ||
          !iter->ReadStringPiece(&account) || !IsValidAccountId(account) ||
          !iter->ReadStringPiece(&proxy) || proxy.size() > 2048 ||
          proxy.find_first_of(";,=@\r\n\0", 0, 7) != std::string_view::npos ||
          !iter->ReadStringPiece(&seed) || !IsValidSeed(seed)) return false;
      // 先验证切片长度再分配，损坏的状态不能触发无界字符串分配。
      parsed = SaasAccountState{std::string(account), std::string(proxy), std::string(seed)};
      if (version == kVersion) {
        std::string_view agent;
        int hardware;
        if (!iter->ReadStringPiece(&agent) || !SaasFingerprintConfig::IsValidUserAgent(agent) ||
            !iter->ReadInt(&hardware) || hardware < 0 || hardware > 64) return false;
        parsed->user_agent = std::string(agent); parsed->hardware_concurrency = hardware;
      }
      if (!iter->ReadInt(&count)) return false;
    }
    if (count < 1 || count > kMaxEntries ||
        !iter->ReadInt(current_entry_index) || *current_entry_index < 0 ||
        *current_entry_index >= count ||
        iter->RemainingBytes() / 8 < static_cast<size_t>(count)) return false;
    *entry_count = count;
    *state = std::move(parsed);
    return true;
  }
};

}  // namespace chrome::android

#endif  // CHROME_BROWSER_UI_ANDROID_TAB_MODEL_SAAS_ACCOUNT_STATE_H_
