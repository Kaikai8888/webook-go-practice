# Webook：在課程專案上練習的後端設計與實作

> 這不是我從零建立的專案。
> 基礎程式碼來自[極客時間 Go 初級工程師訓練營](https://gitee.com/geektime-geekbang_admin/geektime-basic-go)的「小微書（webook）」範例。我拿它當練習場，挑了幾個比較複雜的題目，**自己設計方案並實作**。
>
> **我自己寫的部分都放在下面的 Pull Requests 裡**，每個 PR 的描述會寫出設計考量、取捨和限制。建議直接從 PR 開始看。

## 精選 PR

| # | 主題 | 關鍵字 | 規模 |
|---|------|--------|------|
| [#1](https://github.com/Kaikai8888/geektime-go-basic/pull/1) | 簡訊服務：故障偵測＋非同步 failback | Sliding window、Decorator、背景 retry、並發控制 | +1.4k 行，26 個 commit，含單元測試 |
| [#2](https://github.com/Kaikai8888/geektime-go-basic/pull/2) | 分散式任務調度：把 cronjob 分給負載最低的節點 | Redis ZSET、分散式鎖、自動續約、節點故障處理 | +0.5k 行，含 Redis e2e 測試 |

---

### #1 簡訊服務：Sliding Window 錯誤率偵測＋非同步 Failback

**問題**：第三方簡訊服務變慢或掛掉時，同步呼叫會拖住 API，請求也會遺失。

**方案**：用 Decorator 包住原本的 `sms.Service`，一層處理一件事：

```
AsyncFailBackSmsService        ← 發送失敗時先存進 DB，再交給背景 goroutine 重試
  └─ FaultDetect (sliding window error rate)  ← 判斷服務商是否已經掛掉
       └─ RateLimit (Redis sliding window)     ← 限流
            └─ 實際的 SMS provider
```

**設計重點**
- **用錯誤率判斷，不用「連續失敗 N 次」**：連續 N 次可能拉得很長，例如 N-1 次 timeout 後隔一小時才又來一次，就被當成掛掉了。改成看最近 N 秒的 timeout 比例，而且**樣本數不夠時不判定**，誤判比較少；服務商開始過載、只有部分請求 timeout 時，也能比較早發現。
- **掛掉之後仍保留 10% 流量當探針**：90% 直接轉非同步，10% 照常同步發送。window 往後滑、錯誤率降下來後，就會自動切回同步。
- **自己寫的 `pkg/rate_counter`**：用 linked list 存資料點，背景 job 定期清掉過期資料，避免 memory 一直漲；有加鎖，累計值和資料點會一起更新。樣本數門檻用另一個 decorator（`WithMinDataPointLimit`）加上去。
- **背景 retry**：用 CAS 限制最多同時跑幾個 retry goroutine；沒有待重試的資料時，goroutine 會自己結束；會監聽 SIGINT/SIGTERM，避免關機時「簡訊已重送、DB 還沒更新」，下次又重複發送。DB 記錄重試次數、處理中狀態、最長重試期限，不會隔太久才把簡訊送出去。

**限制與可以改進的地方**：流量低時湊不到樣本數，這套機制就不會作用。可以依尖峰/離峰設定不同的樣本數門檻。

主要程式：`webook/internal/service/sms/failback/`、`webook/internal/service/sms/fault_detect/`、`webook/pkg/rate_counter/`

---

### #2 分散式任務調度：把任務分給負載最低的節點

**問題**：課程的版本是多個節點搶同一把 Redis 分散式鎖，誰搶到誰跑熱榜計算，完全沒考慮哪個節點比較閒。

**方案**
- 每個節點每 15 秒回報自己的 CPU 使用率，寫進 Redis ZSET（`member = instanceId`、`score = load`）。
- **key 依 15 秒時間區間切開**（`load:{biz}:{區間起點 unix nano}`）。排名時**只看上一個完整區間**：
  - 節點當機後不會再回報，最多一到兩個區間後就不會被選中，不會拿當機前的舊資料把任務分給它。
  - 各節點回報的時間點有誤差，看上一個區間可以確保所有節點都回報過了才比較。
- **搶鎖前、每次續約前**都會先確認自己是不是負載最低的節點。不是的話就不續約，讓鎖過期，交給別的節點接手。
- **冷啟動**：沒有任何負載資料時（例如剛啟動），直接搶鎖，確保一定有節點在跑。

**實作**：`pkg/redislockx` 改寫自 [gotomicro/redis-lock](https://github.com/gotomicro/redis-lock) 的 AutoRefresh，加入 `checker` hook，續約前先檢查負載。PR 描述裡有完整的 sequence diagram。

主要程式：`webook/internal/job/ranking.go`、`webook/internal/job/report_load_job.go`、`webook/internal/repository/cache/load.go`、`webook/pkg/redislockx/`

---

## 其他練習（課程作業）

| 分支 | 內容 |
|------|------|
| [`feature/week4_local-code-cache`](https://github.com/Kaikai8888/geektime-go-basic/compare/2ccf386...feature/week4_local-code-cache) | 用 bigcache 實作本地驗證碼快取，取代 Redis：限制發送頻率和驗證次數，並處理並發 |
| [`feature/week3_k8s-basic`](https://github.com/Kaikai8888/geektime-go-basic/tree/feature/week3_k8s-basic/hw/screen-shot/week3) | 把服務、MySQL、Redis、Nginx Ingress 部署到 K8s |

## 技術棧

Go · Gin · GORM / MySQL · Redis · Wire（DI）· gomock · K8s

---

<details>
<summary>原課程 README</summary>

這是極客時間 Go 初級工程師訓練營的配套程式碼。程式碼分成兩部分：基礎語法（`syntax/`）和小微書（`webook/`）。程式碼會隨課程演進，可以用 git log 查看提交紀錄。

</details>
