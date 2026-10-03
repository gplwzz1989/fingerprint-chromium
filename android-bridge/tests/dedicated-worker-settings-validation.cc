#include <cstdio>
#include "third_party/blink/renderer/core/workers/worker_settings.h"

// 执行实际 WorkerSettings 构造和跨级复制，不模拟 Worker 或声明已验证浏览器效果。
int main() {
  int checks = 0, failures = 0;
  const auto verify = [&](bool value) { ++checks; if (!value) ++failures; };
  blink::WorkerSettings parent(nullptr);
  verify(parent.GetFingerprintHardwareConcurrency() == 0);
  for (unsigned value : {1U, 7U, 64U}) {
    parent.SetFingerprintHardwareConcurrency(value);
    auto child = blink::WorkerSettings::Copy(&parent);
    auto nested = blink::WorkerSettings::Copy(child.get());
    verify(child->GetFingerprintHardwareConcurrency() == value);
    verify(nested->GetFingerprintHardwareConcurrency() == value);
    parent.SetFingerprintHardwareConcurrency(value == 7 ? 8 : 7);
    verify(child->GetFingerprintHardwareConcurrency() == value);
    verify(nested->GetFingerprintHardwareConcurrency() == value);
  }
  for (unsigned value : {0U, 65U, 0xffffffffU}) {
    parent.SetFingerprintHardwareConcurrency(value);
    verify(parent.GetFingerprintHardwareConcurrency() == 0);
    verify(blink::WorkerSettings::Copy(&parent)->GetFingerprintHardwareConcurrency() == 0);
  }
  std::printf("专用 Worker 配置快照检查：%d 项，失败 %d 项\n", checks, failures);
  return failures ? 1 : 0;
}
