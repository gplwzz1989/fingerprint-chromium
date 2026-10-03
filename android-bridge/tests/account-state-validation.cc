// 测试实际分区状态头和 Chromium Pickle；尾部只用于头解析的长度预算，
// 不将这些测试字段当作真实导航或 Android 生命周期验收。
#include "chrome/browser/ui/android/tab_model/saas_account_state.h"

#include <cstdio>
#include <limits>
#include <vector>

using chrome::android::SaasAccountState;

namespace {
int checks = 0;
int failures = 0;

void Verify(bool actual, bool expected) {
  ++checks;
  if (actual != expected) {
    ++failures;
    std::fprintf(stderr, "账号状态校验结果不一致：第 %d 项\n", checks);
  }
}

void WriteTail(base::Pickle* pickle, int count) {
  for (int i = 0; i < count; ++i) {
    pickle->WriteInt(sizeof(int));
    pickle->WriteInt(0);
  }
}

base::Pickle MakeRaw(const SaasAccountState& state, int version = 1,
                     bool off_the_record = false, int count = 1, int index = 0) {
  base::Pickle pickle;
  pickle.WriteBool(off_the_record);
  pickle.WriteInt(0);
  pickle.WriteInt(SaasAccountState::kMarker);
  pickle.WriteInt(version);
  pickle.WriteString(state.account_id);
  pickle.WriteString(state.proxy_rules);
  pickle.WriteString(state.fingerprint_seed);
  pickle.WriteInt(count);
  pickle.WriteInt(index);
  WriteTail(&pickle, 1);
  return pickle;
}

bool Read(const base::Pickle& pickle, std::optional<SaasAccountState>* state,
          int* count, int* index) {
  base::PickleIterator iter(pickle);
  bool off_the_record = false;
  return SaasAccountState::ReadHeader(&iter, &off_the_record, count, index, state);
}
}  // namespace

int main() {
  const SaasAccountState original{"account-01", "socks5://127.0.0.1:1080", "4294967295"};
  std::optional<SaasAccountState> state;
  int count = 0, index = -1;
  base::Pickle pickle;
  Verify(SaasAccountState::WriteHeader(&pickle, false, 2, 1, original), true);
  WriteTail(&pickle, 2);
  Verify(Read(pickle, &state, &count, &index), true);
  Verify(state && state->account_id == original.account_id &&
         state->proxy_rules == original.proxy_rules &&
         state->fingerprint_seed == original.fingerprint_seed &&
         count == 2 && index == 1, true);
  base::PickleIterator legacy_iter(pickle);
  bool legacy_off_the_record = true;
  int legacy_count = -1, legacy_index = 0;
  Verify(legacy_iter.ReadBool(&legacy_off_the_record) &&
         legacy_iter.ReadInt(&legacy_count) && legacy_iter.ReadInt(&legacy_index), true);
  Verify(legacy_count == 0 && legacy_index < 0, true);

  for (bool off_the_record : {false, true}) {
    base::Pickle ordinary;
    Verify(SaasAccountState::WriteHeader(&ordinary, off_the_record, 1, 0, std::nullopt), true);
    WriteTail(&ordinary, 1);
    Verify(Read(ordinary, &state, &count, &index), true);
    Verify(!state, true);
  }
  for (const auto& id : std::vector<std::string>{"", "../account", "账号", "a b",
           std::string(129, 'a'), std::string("a\0b", 3)}) {
    auto invalid = original;
    invalid.account_id = id;
    state = original;
    Verify(Read(MakeRaw(invalid), &state, &count, &index), false);
    Verify(!state, true);
  }
  for (const auto& seed : std::vector<std::string>{"", "-1", "+1", " 1", "1.1",
           "4294967296", std::string("1\0", 2)}) {
    auto invalid = original;
    invalid.fingerprint_seed = seed;
    Verify(Read(MakeRaw(invalid), &state, &count, &index), false);
  }
  for (const auto& proxy : std::vector<std::string>{"a:80;b:81", "a:80,b:81",
           "user:password@proxy:80", "a:80\r\n", std::string("a:80\0", 5),
           std::string(2049, 'a')}) {
    auto invalid = original;
    invalid.proxy_rules = proxy;
    Verify(Read(MakeRaw(invalid), &state, &count, &index), false);
  }
  Verify(Read(MakeRaw(original, 0), &state, &count, &index), false);
  Verify(Read(MakeRaw(original, 2), &state, &count, &index), false);
  Verify(Read(MakeRaw(original, 1, true), &state, &count, &index), false);
  for (int entries : {-1, 0, 10001, std::numeric_limits<int>::max()}) {
    Verify(Read(MakeRaw(original, 1, false, entries), &state, &count, &index), false);
  }
  for (int current : {-1, 1, std::numeric_limits<int>::max()}) {
    Verify(Read(MakeRaw(original, 1, false, 1, current), &state, &count, &index), false);
  }
  Verify(Read(MakeRaw(original, 1, false, 2, 0), &state, &count, &index), false);
  base::Pickle truncated;
  truncated.WriteBool(false);
  truncated.WriteInt(0);
  truncated.WriteInt(SaasAccountState::kMarker);
  truncated.WriteInt(SaasAccountState::kVersion);
  truncated.WriteString(original.account_id);
  Verify(Read(truncated, &state, &count, &index), false);
  base::Pickle unknown_marker;
  unknown_marker.WriteBool(false);
  unknown_marker.WriteInt(0);
  unknown_marker.WriteInt(SaasAccountState::kMarker + 1);
  WriteTail(&unknown_marker, 1);
  Verify(Read(unknown_marker, &state, &count, &index), false);
  base::Pickle rejected;
  Verify(SaasAccountState::WriteHeader(&rejected, true, 1, 0, original), false);
  Verify(rejected.payload_size() == 0, true);
  std::printf("账号状态头校验：%d 项，失败 %d 项\n", checks, failures);
  return failures ? 1 : 0;
}
