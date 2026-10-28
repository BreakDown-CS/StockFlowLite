# คู่มือทดสอบ API — StockFlow Lite (Phase 1–9)

> เอกสารนี้ใช้สำหรับ **ทดสอบ API ด้วยมือ/สคริปต์** บนเครื่อง local (PostgreSQL 18, Go 1.27, Fiber v2)
> ผลทดสอบล่าสุดที่บันทึกไว้: **51/51 PASS, 0 FAIL** (2026-10-28) — รายละเอียดดู §9

---

## 1. เตรียมสภาพแวดล้อม

```powershell
cd D:\WORK-CS\AI-CS\stock_flow_lite

# 1) ตรวจว่ารัน migration ครบแล้ว
go run ./cmd/migrate -action status
#    ทุกเวอร์ชัน (000001–000007) ต้องเป็น YES

# 2) ตรวจ config (ต้องไม่ error)
go run ./cmd/maintenance -action check-stock

# 3) รัน API (พอร์ตตาม APP_PORT ใน .env หรือ 8080)
go run ./cmd/api
```

เงื่อนไขที่สำคัญ

| เงื่อนไข | ผลถ้าไม่ผ่าน |
|---|---|
| `AUTH_TOKEN_PEPPER` ยาว **≥ 16 ตัวอักษร** | แอปไม่ยอม start: `AUTH_TOKEN_PEPPER must be at least 16 characters` |
| `DB_PASSWORD` / `AUTH_TOKEN_PEPPER` ไม่ใช่ค่า dev เมื่อ `APP_ENV != development` | แอปไม่ยอม start (fail-closed) |
| มี user อย่างน้อย 1 คน (ADMIN) | login ไม่ได้ (dev identities ถูกปิดใช้งาน) |

> ⚠️ **กับดัก cmd.exe:** `set VAR=value & next` จะได้ค่า `"value "` (มีช่องว่างต่อท้าย) ทำให้ login ไม่ผ่าน
> ให้ใช้ `set "VAR=value"& next` หรือ `set VAR=value&& next` เสมอ

---

## 2. สร้างผู้ดูแลระบบคนแรก (bootstrap admin)

```cmd
:: Windows cmd — สังเกตเครื่องหมายคำพูดปิดค่า
set "BOOTSTRAP_ADMIN_USERNAME=admin"& set "BOOTSTRAP_ADMIN_PASSWORD=<รหัสผ่าน ≥ 8 ตัว>"& go run ./cmd/user -action create
```

```powershell
# PowerShell
$env:BOOTSTRAP_ADMIN_USERNAME="admin"
$env:BOOTSTRAP_ADMIN_PASSWORD="<รหัสผ่าน ≥ 8 ตัว>"
go run ./cmd/user -action create
```

เปลี่ยน/รีเซ็ตรหัสภายหลัง

```cmd
set "BOOTSTRAP_ADMIN_USERNAME=admin"& set "BOOTSTRAP_ADMIN_PASSWORD=<รหัสใหม่>"& go run ./cmd/user -action reset-password
```

> CLI พิมพ์แค่ `username` และ `id` — ไม่พิมพ์รหัสผ่าน

---

## 3. รูปแบบ response / สิทธิ์ / error code

### 3.1 Envelope

```json
{ "success": true,  "message": "…", "data": { }, "meta": { "limit": 20, "offset": 0, "total": 1 } }
{ "success": false, "error": { "code": "…", "message": "…", "details": { } } }
```

- `/health`, `/ready` **ไม่ใช้** envelope (ตอบ JSON ตรง ๆ)
- รายงาน (`/reports/*`) ใช้ `data = { "summary": {...}, "items": [...] }`

### 3.2 การยืนยันตัวตน

```http
Authorization: Bearer <token>
```

- token จาก `POST /api/v1/auth/login` (อายุตาม `AUTH_TOKEN_TTL` default 12h)
- logout / เปลี่ยนรหัส / ถูกปิดบัญชี → token ใช้ไม่ได้ทันที

### 3.3 สิทธิ์ (role matrix)

| กลุ่ม endpoint | REQUESTER | STOREKEEPER | ADMIN |
|---|---|---|---|
| `/health`, `/ready`, `POST /auth/login` | public | public | public |
| `/auth/logout`, `/auth/me`, `/auth/change-password` | ✅ | ✅ | ✅ |
| `GET /products`, `GET /products/:id`, `GET /stock-movements` | ✅ | ✅ | ✅ |
| `POST/PUT/PATCH /products`, `POST /stock-receipts` | ❌ 403 | ✅ | ✅ |
| `POST /stock-requests`, `GET /stock-requests/*` | ✅ (เฉพาะของตัวเอง) | ✅ | ✅ |
| `POST /:id/approve`, `/reject`, `/issue` | ❌ 403 | ✅ | ✅ |
| `POST /:id/cancel` | ✅ เฉพาะของตัวเอง | ✅ | ✅ |
| `/users/*`, `/audit-events`, `/reports/integrity/stock` | ❌ 403 | ❌ 403 | ✅ |
| `/reports/inventory`, `/reports/stock-movements`, `/reports/stock-requests` | ✅ | ✅ | ✅ |

กฎเพิ่ม: ผู้ขอ **ห้ามอนุมัติ/ปฏิเสธ/จ่ายใบของตัวเอง** → `403 SELF_APPROVAL_FORBIDDEN`

### 3.4 Error code ที่ต้องตรวจ

| Status | code | เกิดเมื่อ |
|---|---|---|
| 400 | `BAD_REQUEST` | body ไม่ใช่ JSON |
| 400 | `VALIDATION_ERROR` | ค่าไม่ผ่านกฎ (quantity ≤ 0, notes/reason ยาวเกิน, password สั้น/ยาวเกิน) |
| 400 | `INVALID_ID` / `INVALID_PARAMETER` | UUID ผิด, limit/offset/status/date ผิด |
| 400 | `PRODUCT_INACTIVE` | รับเข้า/สร้างใบขอ/จ่ายของกับสินค้าปิดใช้งาน |
| 400 | `WEAK_PASSWORD` / `PASSWORD_REUSED` | รหัสผ่านอ่อน / ตั้งซ้ำรหัสเดิม |
| 401 | `UNAUTHORIZED` | ไม่มี/ผิดรูปแบบ/หมดอายุ/ถูก revoke token |
| 401 | `INVALID_CREDENTIALS` | username/password ผิด (ไม่บอกว่าใครผิด) |
| 403 | `FORBIDDEN` | role ไม่พอ |
| 403 | `SELF_APPROVAL_FORBIDDEN` | อนุมัติ/จ่ายใบตัวเอง |
| 404 | `NOT_FOUND` | ไม่พบสินค้า/ใบขอ/ผู้ใช้ |
| 409 | `DUPLICATE_SKU` / `DUPLICATE_USERNAME` | ค่าซ้ำ |
| 409 | `INVALID_STATE` | สถานะใบขอไม่ถูกต้อง (อนุมัติซ้ำ/จ่ายซ้ำ/ยกเลิกซ้ำ) |
| 409 | `INSUFFICIENT_STOCK` | สต็อกไม่พอ (มี `details.items`) |
| 409 | `IDEMPOTENCY_KEY_REUSED` | ใช้ key เดิมกับใบอื่น |
| 409 | `LAST_ADMIN` | ปลด/ปิด ADMIN คนสุดท้าย |
| 429 | `TOO_MANY_ATTEMPTS` | login ผิดติดกันเกินกำหนด |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด (ไม่ leak SQL) |

---

## 4. Test Matrix (ผลรันจริง 51/51 PASS)

ตัวย่อ: **A** = token ของ ADMIN, **K** = token ของ STOREKEEPER, **–** = ไม่ส่ง token

| # | Method & Path | token | คาดหวัง | จุดที่ต้องตรวจ |
|---|---|---|---|---|
| 1 | `GET /health` | – | 200 | `{"status":"ok","service":"stockflow-lite"}` |
| 2 | `GET /ready` | – | 200 | `{"status":"ready","database":"up"}` (503 ถ้า DB ล่ม) |
| 3 | `POST /auth/login` (รหัสผิด) | – | 401 | `error.code = INVALID_CREDENTIALS` |
| 4 | `POST /auth/login` (ADMIN) | – | 200 | `data.token`, `data.expires_at`, `data.user.role=ADMIN` |
| 5 | `GET /auth/me` | A | 200 | `data.username` ตรง, ไม่มี `password_hash` |
| 6 | `GET /products` | – | 401 | `error.code = UNAUTHORIZED` |
| 7 | `POST /products` (A) | A | 201 | `data.current_stock = 0`, `is_active = true` |
| 8 | `POST /products` (B) | A | 201 | – |
| 9 | `POST /products` (SKU ซ้ำ) | A | 409 | `DUPLICATE_SKU` |
| 10 | `GET /products?search=` | A | 200 | `meta.total ≥ 1`, เจอ SKU ที่ค้น |
| 11 | `GET /products/:id` | A | 200 | `data.id` ตรง |
| 12 | `GET /products/:id` (ไม่มีจริง) | A | 404 | `NOT_FOUND` |
| 13 | `GET /products/:id` (uuid ผิด) | A | 400 | `INVALID_ID` |
| 14 | `PUT /products/:id` | A | 200 | ชื่อ/`min_stock` เปลี่ยนตามที่ส่ง |
| 15 | `POST /stock-receipts` (+50) | A | 201 | `data.product.current_stock` เพิ่ม, `data.movement.movement_type=RECEIPT` |
| 16 | `POST /stock-receipts` (+10) | A | 201 | – |
| 17 | `POST /stock-receipts` (quantity 0) | A | 400 | `VALIDATION_ERROR`, สต็อกไม่เปลี่ยน |
| 18 | `GET /stock-movements?product_id=` | A | 200 | มี movement ของสินค้า, `meta.total` |
| 19 | `GET /stock-movements?from=&to=` | A | 200 | กรองช่วงวันที่ได้ (inclusive) |
| 20 | `GET /stock-movements?from=yesterday` | A | 400 | `INVALID_PARAMETER` |
| 21 | `POST /users` (STOREKEEPER) | A | 201 | `data.role = STOREKEEPER` |
| 22 | `GET /users?limit=5` | A | 200 | `meta {limit,offset,total}` |
| 23 | `GET /users/:id` | A | 200 | `data.id` ตรง |
| 24 | `POST /auth/login` (STOREKEEPER) | – | 200 | ได้ token K |
| 25 | `GET /users` | K | 403 | `FORBIDDEN` |
| 26 | `GET /audit-events` | K | 403 | `FORBIDDEN` |
| 27 | `GET /reports/integrity/stock` | K | 403 | `FORBIDDEN` (ADMIN เท่านั้น) |
| 28 | `POST /stock-requests` (2 items) | A | 201 | `status=PENDING`, `items` ครบ, ไม่ตัดสต็อก |
| 29 | `GET /stock-requests/:id` | A | 200 | `data.items` ครบ |
| 30 | `GET /stock-requests?status=PENDING` | A | 200 | เจอใบที่สร้าง |
| 31 | `GET /stock-requests?status=BOGUS` | A | 400 | `INVALID_PARAMETER` |
| 32 | `POST /stock-requests/:id/approve` | A (ผู้ขอ) | 403 | `SELF_APPROVAL_FORBIDDEN`, สถานะยัง PENDING |
| 33 | `POST /stock-requests/:id/approve` | K | 200 | `status=APPROVED`, `reviewer_id`, `reviewed_at` |
| 34 | `POST /stock-requests/:id/approve` (ซ้ำ) | K | 409 | `INVALID_STATE` |
| 35 | `POST /stock-requests/:id/issue` + `Idempotency-Key` | K | 200 | `status=ISSUED`, `movements` 1 แถวต่อ item, `meta.issuance_id` |
| 36 | `POST /stock-requests/:id/issue` (key เดิม) | K | 200 | `meta.idempotent_replay = true`, movement id เดิม, สต็อกไม่ถูกตัดซ้ำ |
| 37 | `POST /stock-requests/:id/issue` (ไม่มี key) | K | 409 | `INVALID_STATE` (`details.current_status=ISSUED`) |
| 38 | `POST /stock-requests/:id/cancel` (+reason) | A (ผู้ขอ) | 200 | `status=CANCELLED`, reason ถูกบันทึก |
| 39 | `POST /stock-requests/:id/cancel` (ซ้ำ) | A | 409 | `INVALID_STATE` |
| 40 | `POST /stock-requests/:id/reject` | K | 200 | `status=REJECTED`, `rejection_reason` |
| 41 | `GET /reports/inventory` | A | 200 | `data.summary` + `data.items` + `meta` |
| 42 | `GET /reports/inventory?low_stock=maybe` | A | 400 | `INVALID_PARAMETER` |
| 43 | `GET /reports/stock-movements?product_id=` | A | 200 | `summary.receipt_total_quantity/issue_total_quantity/net_change` |
| 44 | `GET /reports/stock-requests` | A | 200 | `summary` นับตามสถานะ, `issued_units` ของใบ ISSUED |
| 45 | `GET /reports/integrity/stock?only_drifted=true` | A | 200 | หลัง workflow ต้องได้ `drifted_products = 0` |
| 46 | `GET /audit-events?limit=5` | A | 200 | มี `REQUEST_APPROVED`, `REQUEST_ISSUED` ฯลฯ |
| 47 | `PATCH /users/:id` (`is_active=false`) | A | 200 | `data.is_active=false` |
| 48 | `POST /users/:id/reset-password` | A | 200 | revoke session ของผู้ใช้คนนั้น |
| 49 | `GET /auth/me` ด้วย token K (หลังถูกปิดบัญชี) | K | 401 | token ถูก revoke |
| 50 | `POST /auth/logout` | A | 200 | – |
| 51 | `GET /auth/me` หลัง logout | A | 401 | `UNAUTHORIZED` |

---

## 5. ตัวอย่างคำสั่งทดสอบ (PowerShell)

### 5.1 Helper สำหรับเรียก API (คัดลอกไปใช้ได้เลย)

```powershell
Add-Type -AssemblyName System.Net.Http | Out-Null
$base   = "http://localhost:8080/api/v1"
$client = New-Object System.Net.Http.HttpClient
$client.Timeout = [TimeSpan]::FromSeconds(30)

function Send($method, $path, $body = $null, $token = $null, $headers = $null) {
    $uri = if ($path.StartsWith('http')) { $path } else { "$base$path" }
    $req = New-Object System.Net.Http.HttpRequestMessage((New-Object System.Net.Http.HttpMethod($method)), $uri)
    if ($token)   { $req.Headers.Authorization = New-Object System.Net.Http.Headers.AuthenticationHeaderValue('Bearer', $token) }
    if ($headers) { foreach ($k in $headers.Keys) { [void]$req.Headers.TryAddWithoutValidation($k, $headers[$k]) } }
    if ($null -ne $body) {
        $json = if ($body -is [string]) { $body } else { $body | ConvertTo-Json -Depth 8 -Compress }
        $req.Content = New-Object System.Net.Http.StringContent($json, [System.Text.Encoding]::UTF8, 'application/json')
    }
    $resp = $client.SendAsync($req).GetAwaiter().GetResult()
    $text = $resp.Content.ReadAsStringAsync().GetAwaiter().GetResult()
    $out  = $null; if ($text) { try { $out = $text | ConvertFrom-Json } catch {} }
    [pscustomobject]@{ Status = [int]$resp.StatusCode; Body = $out; Raw = $text }
}

$auth  = Send 'POST' '/auth/login' @{ username = 'admin'; password = '<รหัสผ่าน>' }
$token = $auth.Body.data.token
$auth.Status   # ต้องเป็น 200
```

### 5.2 Health / Auth

```powershell
Send 'GET'  'http://localhost:8080/health'                                # 200
Send 'GET'  'http://localhost:8080/ready'                                 # 200 (503 = DB ล่ม)
Send 'POST' '/auth/login' @{ username='admin'; password='wrong-pass' }   # 401 INVALID_CREDENTIALS
Send 'GET'  '/auth/me' $null $token                                       # 200
Send 'POST' '/auth/change-password' @{ current_password='<เดิม>'; new_password='<ใหม่>' } $token
Send 'POST' '/auth/logout' $null $token                                   # 200 (token ถูก revoke)
Send 'GET'  '/auth/me' $null $token                                       # 401
```

### 5.3 Products

```powershell
$pa = Send 'POST' '/products' @{ sku='API-DEMO-A'; name='Demo A'; unit='BOX'; min_stock=5 } $token
$prodA = $pa.Body.data.id

$pb = Send 'POST' '/products' @{ sku='API-DEMO-B'; name='Demo B'; unit='PCS'; min_stock=0 } $token
$prodB = $pb.Body.data.id

Send 'POST' '/products' @{ sku='API-DEMO-A'; name='dup' } $token                          # 409 DUPLICATE_SKU
Send 'GET'  '/products?search=API-DEMO&is_active=all' $null $token                       # 200
Send 'GET'  "/products/$prodA" $null $token                                               # 200
Send 'GET'  '/products/00000000-0000-4000-8000-0000000000ff' $null $token                 # 404
Send 'GET'  '/products/not-a-uuid' $null $token                                           # 400
Send 'PUT'  "/products/$prodA" @{ name='Demo A v2'; unit='BOX'; min_stock=6 } $token      # 200
Send 'PATCH' "/products/$prodB/deactivate" $null $token                                   # 200 (ใช้หลังจบการทดสอบ)
```

### 5.4 Stock receipts / movements

```powershell
Send 'POST' '/stock-receipts' @{ product_id=$prodA; quantity=50; notes='API test' } $token  # 201
Send 'POST' '/stock-receipts' @{ product_id=$prodB; quantity=10 } $token                    # 201
Send 'POST' '/stock-receipts' @{ product_id=$prodA; quantity=0 } $token                     # 400

Send 'GET' "/stock-movements?product_id=$prodA&limit=5" $null $token                       # 200
$from = (Get-Date).AddHours(-2).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
$to   = (Get-Date).AddHours(1).ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ')
Send 'GET' "/stock-movements?from=$from&to=$to" $null $token                               # 200
Send 'GET' '/stock-movements?from=yesterday' $null $token                                   # 400
```

### 5.5 Users (เฉพาะ ADMIN)

```powershell
$u = Send 'POST' '/users' @{ username='demo_keeper'; password='Demo-Keeper-2026'; role='STOREKEEPER' } $token
$keepId = $u.Body.data.id                                                                    # 201

Send 'GET'   '/users?limit=5&role=STOREKEEPER' $null $token                                 # 200
Send 'GET'   "/users/$keepId" $null $token                                                  # 200
Send 'PATCH' "/users/$keepId" @{ is_active=$false } $token                                  # 200 (revoke session ของ user นี้)
Send 'POST'  "/users/$keepId/reset-password" @{ new_password='Demo-Keeper-2027' } $token    # 200
```

### 5.6 Stock requests (workflow หลัก)

```powershell
# ผู้ขอ = admin (A) — สร้างใบขอ PENDING
$r1 = Send 'POST' '/stock-requests' @{
    items = @(
        @{ product_id = $prodA; quantity = 10 },
        @{ product_id = $prodB; quantity = 5 }
    )
    notes = 'API test request'
} $token
$reqId = $r1.Body.data.id                                                                   # 201, status=PENDING

Send 'GET' "/stock-requests/$reqId" $null $token                                            # 200 (items ครบ)
Send 'GET' '/stock-requests?status=PENDING&limit=5' $null $token                           # 200
Send 'GET' '/stock-requests?status=BOGUS' $null $token                                      # 400

# สลับไปใช้ token ของ STOREKEEPER (K) เพื่ออนุมัติ/จ่ายของ
$keepLogin = Send 'POST' '/auth/login' @{ username='demo_keeper'; password='Demo-Keeper-2027' }
$tokenK = $keepLogin.Body.data.token

Send 'POST' "/stock-requests/$reqId/approve" $null $token                                   # 403 SELF_APPROVAL_FORBIDDEN
Send 'POST' "/stock-requests/$reqId/approve" $null $tokenK                                  # 200 APPROVED
Send 'POST' "/stock-requests/$reqId/approve" $null $tokenK                                  # 409 INVALID_STATE

Send 'POST' "/stock-requests/$reqId/issue" $null $tokenK @{ 'Idempotency-Key' = 'demo-key-0001' }  # 200 ISSUED
Send 'POST' "/stock-requests/$reqId/issue" $null $tokenK @{ 'Idempotency-Key' = 'demo-key-0001' }  # 200 replay
Send 'POST' "/stock-requests/$reqId/issue" $null $tokenK                                     # 409 (ไม่มี key)

# ยกเลิก (ผู้ขอ) และปฏิเสธ (storekeeper)
$r2 = Send 'POST' '/stock-requests' @{ items = @(@{ product_id=$prodA; quantity=1 }) } $token
Send 'POST' "/stock-requests/$($r2.Body.data.id)/cancel" @{ reason='เปลี่ยนความต้องการ' } $token   # 200 CANCELLED
Send 'POST' "/stock-requests/$($r2.Body.data.id)/cancel" $null $token                              # 409

$r3 = Send 'POST' '/stock-requests' @{ items = @(@{ product_id=$prodA; quantity=2 }) } $token
Send 'POST' "/stock-requests/$($r3.Body.data.id)/reject" @{ rejection_reason='งบไม่พอ' } $tokenK    # 200 REJECTED
```

### 5.7 Reports / Audit

```powershell
Send 'GET' '/reports/inventory?limit=5&low_stock=true' $null $token                        # 200
Send 'GET' "/reports/stock-movements?product_id=$prodA" $null $token                      # 200 + summary
Send 'GET' '/reports/stock-requests?status=ISSUED' $null $token                            # 200 + summary
Send 'GET' '/reports/integrity/stock?only_drifted=true' $null $token                      # 200, ต้องได้ drifted_products = 0
Send 'GET' '/audit-events?limit=10&event_type=REQUEST_ISSUED' $null $token                # 200
```

### 5.8 ตรวจสต็อกกับ ledger ด้วย CLI (ไม่ต้องผ่าน HTTP)

```bash
go run ./cmd/maintenance -action check-stock          # exit 0 = ไม่มี drift, exit 2 = พบ drift
go run ./cmd/maintenance -action check-stock -only-drifted
go run ./cmd/maintenance -action purge-sessions -older-than 168h
```

---

## 6. Workflow ครบวงจร (ลำดับที่ต้องเรียก)

```
1) POST /auth/login                       → token (ADMIN)
2) POST /products  ×2                    → prodA, prodB
3) POST /stock-receipts (A, B)           → สต็อกเพิ่ม + movement RECEIPT
4) POST /users (STOREKEEPER)             → login รับ token K
5) POST /stock-requests (items: A, B)    → PENDING
6) POST /:id/approve  (token K)          → APPROVED (ยังไม่ตัดสต็อก)
7) POST /:id/issue    (token K + Idempotency-Key) → ISSUED + movement ISSUE
8) GET  /reports/integrity/stock         → drift = 0
9) GET  /audit-events                    → เห็น REQUEST_CREATED/APPROVED/ISSUED
10) POST /auth/logout                    → token ใช้ไม่ได้
```

> จำไว้ว่า `approve` **ไม่ตัดสต็อก** — ต้องเรียก `issue` ต่อ (ยกเว้นใบถูก cancel/reject)

---

## 7. Negative & Security tests (ต้องทดสอบเสมอ)

| # | สถานการณ์ | วิธีทดสอบ | คาดหวัง |
|---|---|---|---|
| N1 | ไม่มี token | `Send 'GET' '/products'` | 401 `UNAUTHORIZED` |
| N2 | token ปลอม | `Send 'GET' '/products' $null 'fake-token'` | 401 |
| N3 | หลัง logout | ใช้ token เดิมเรียก `/auth/me` | 401 |
| N4 | STOREKEEPER เข้า `/users` | `Send 'GET' '/users' $null $tokenK` | 403 `FORBIDDEN` |
| N5 | REQUESTER รับสินค้าเข้า | login ผู้ใช้ role REQUESTER → `POST /stock-receipts` | 403 |
| N6 | REQUESTER เห็นใบคนอื่น | `GET /stock-requests` ด้วย token REQUESTER | เห็นเฉพาะใบของตัวเอง (หรือว่าง) |
| N7 | ผู้ขออนุมัติใบตัวเอง | `POST /:id/approve` ด้วย token ผู้ขอ | 403 `SELF_APPROVAL_FORBIDDEN` |
| N8 | อนุมัติซ้ำ | `POST /:id/approve` อีกครั้ง | 409 `INVALID_STATE` |
| N9 | จ่ายของซ้ำ (ไม่มี key) | `POST /:id/issue` อีกครั้ง | 409 + `details.current_status=ISSUED` |
| N10 | สต็อกไม่พอ | ตั้งสต็อก 3 แล้วขอ 10 → approve → issue | 409 `INSUFFICIENT_STOCK` + `details.items`, ยอดไม่ขยับ |
| N11 | สินค้าถูกปิดหลังอนุมัติ | `PATCH /products/:id/deactivate` แล้ว `issue` | 409 `PRODUCT_INACTIVE` |
| N12 | product_id ไม่มีจริง | `POST /stock-requests` ด้วย uuid สุ่ม | 404 `NOT_FOUND` |
| N13 | quantity ≤ 0 | `POST /stock-receipts` quantity 0 / -1 | 400 `VALIDATION_ERROR` |
| N14 | login ผิดติดกัน | ยิงผิด 9 ครั้งจาก IP เดียวกัน | ครั้งที่ 9 → 429 `TOO_MANY_ATTEMPTS` |
| N15 | รหัสผ่านอ่อน/ซ้ำ | `POST /users` password `short`; `change-password` เป็นรหัสเดิม | 400 `WEAK_PASSWORD` / `PASSWORD_REUSED` |
| N16 | ปลด ADMIN คนสุดท้าย | มี ADMIN 1 คน → `PATCH /users/:id` `{is_active:false}` | 409 `LAST_ADMIN` |
| N17 | ใช้ idempotency key ข้ามใบ | ใบ A ใช้ key K แล้วนำ K ไปยิงใบ B | 409 `IDEMPOTENCY_KEY_REUSED` |
| N18 | body ไม่ใช่ JSON | `-d '{invalid'` | 400 `BAD_REQUEST` |
| N19 | ตรวจ log | ดู log ของ API ระหว่างเทสต์ | ไม่มี token/password/hash/pepper/SQL |

ตัวอย่าง curl (bash) สำหรับ N1/N3:
```bash
BASE=http://localhost:8080/api/v1
curl -s -o /dev/null -w "%{http_code}\n" $BASE/products                                      # 401
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOKEN" $BASE/auth/me      # 200
curl -s -X POST -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOKEN" $BASE/auth/logout
curl -s -o /dev/null -w "%{http_code}\n" -H "Authorization: Bearer $TOKEN" $BASE/auth/me      # 401
```

---

## 8. ผลการทดสอบล่าสุด (อ้างอิง)

ทดสอบผ่าน HTTP จริงบน API ที่รันด้วย `.env` ของเครื่องนี้ (สร้าง ADMIN + STOREKEEPER ชั่วคราว แล้วล้างข้อมูลทิ้ง)

```
=== StockFlow Lite API test run 20261028145506 ===
PASS  GET /health                         200     PASS  POST /stock-requests R1              201
PASS  GET /ready                          200     PASS  POST /:id/approve self              403
PASS  POST /auth/login wrong password     401     PASS  POST /:id/approve storekeeper       200
PASS  POST /auth/login admin              200     PASS  POST /:id/approve twice             409
PASS  GET /auth/me                        200     PASS  POST /:id/issue                     200 (movements=2)
PASS  GET /products without token         401     PASS  POST /:id/issue replay              200 (replay=True)
PASS  POST /products ×2                   201     PASS  POST /:id/issue after issued        409
PASS  POST /products duplicate SKU        409     PASS  POST /:id/cancel                    200
PASS  GET /products?search                200     PASS  POST /:id/cancel twice              409
PASS  GET /products/:id                   200     PASS  POST /:id/reject                    200
PASS  GET /products/:id missing           404     PASS  GET /reports/inventory             200
PASS  GET /products/:id bad uuid          400     PASS  GET /reports/stock-movements       200
PASS  PUT /products/:id                   200     PASS  GET /reports/stock-requests        200
PASS  POST /stock-receipts +50/+10        201     PASS  GET /reports/integrity/stock       200
PASS  POST /stock-receipts zero qty       400       └─ drifted_products = 0, absolute_drift = 0
PASS  GET /stock-movements (3 แบบ)      200/400   PASS  GET /audit-events                  200
PASS  POST /users storekeeper             201     PASS  PATCH /users/:id                   200
PASS  GET /users, /users/:id              200     PASS  POST /users/:id/reset-password     200
PASS  POST /auth/login storekeeper        200     PASS  GET /auth/me (revoked)             401
PASS  /users /audit /integrity as K       403     PASS  POST /auth/logout                  200
                                                  PASS  GET /auth/me after logout          401

RESULT pass=51 fail=0
```

> การทดสอบนี้ **ไม่ทิ้งข้อมูลจริงค้างไว้**: ลบ products/requests/users/sessions/audit ของรอบทดสอบแล้ว
> (ตรวจแล้วเหลือ users = 2 dev identities, requests = 0)

---

## 9. ล้างข้อมูลทดสอบ (ทำหลังเทสด้วยมือ)

> ใช้เฉพาะข้อมูลที่คุณสร้างเพื่อทดสอบ (SKU ขึ้นต้นด้วย `API-DEMO` / `APITEST` และ username `demo_*` / `api_test*`)

```sql
BEGIN;
DELETE FROM stock_movements
 WHERE product_id IN (SELECT id FROM products WHERE sku LIKE 'API-DEMO%' OR sku LIKE 'APITEST%');
DELETE FROM stock_request_issuances
 WHERE request_id IN (SELECT id FROM stock_requests
                       WHERE requester_id IN (SELECT id FROM users WHERE username LIKE 'demo_%' OR username LIKE 'api_test%'));
DELETE FROM stock_request_items
 WHERE request_id IN (SELECT id FROM stock_requests
                       WHERE requester_id IN (SELECT id FROM users WHERE username LIKE 'demo_%' OR username LIKE 'api_test%'));
DELETE FROM stock_requests
 WHERE requester_id IN (SELECT id FROM users WHERE username LIKE 'demo_%' OR username LIKE 'api_test%');
DELETE FROM audit_events
 WHERE actor_id IN (SELECT id FROM users WHERE username LIKE 'demo_%' OR username LIKE 'api_test%')
    OR entity_id IN (SELECT id FROM products WHERE sku LIKE 'API-DEMO%' OR sku LIKE 'APITEST%');
DELETE FROM auth_sessions
 WHERE user_id IN (SELECT id FROM users WHERE username LIKE 'demo_%' OR username LIKE 'api_test%');
DELETE FROM products WHERE sku LIKE 'API-DEMO%' OR sku LIKE 'APITEST%';
DELETE FROM users    WHERE username LIKE 'demo_%' OR username LIKE 'api_test%';
COMMIT;
```

ตรวจผล:
```sql
SELECT (SELECT COUNT(*) FROM users) AS users,
       (SELECT COUNT(*) FROM products) AS products,
       (SELECT COUNT(*) FROM stock_requests) AS requests,
       (SELECT COUNT(*) FROM stock_movements) AS movements;
```

---

## 10. Checklist + กับดักที่พบบ่อย

**ก่อนเริ่ม**
- [ ] `go run ./cmd/migrate -action status` → YES ครบ
- [ ] `.env` มี `AUTH_TOKEN_PEPPER` **≥ 16 ตัว**
- [ ] มี ADMIN อย่างน้อย 1 คน (`cmd/user -action create`)
- [ ] `go run ./cmd/api` ขึ้น `HTTP server listening`

**ระหว่าง/หลังเทส**
- [ ] 51 เคสใน §4 ผ่าน
- [ ] `go run ./cmd/maintenance -action check-stock` → ไม่มี drift
- [ ] log ไม่มี token/password/SQL
- [ ] ล้างข้อมูลทดสอบตาม §9

**กับดักที่เจอจริง**

| # | อาการ | สาเหตุ / วิธีแก้ |
|---|---|---|
| 1 | login ตอบ 401 ทั้งที่รหัสถูก | `set VAR=value & next` เติมช่องว่างท้ายค่า → ใช้ `set "VAR=value"& next` หรือ PowerShell `$env:VAR="value"` |
| 2 | แอปไม่ start: `AUTH_TOKEN_PEPPER must be at least 16 characters` | ตั้ง pepper ให้ยาว ≥ 16 (fail-closed ตาม Phase 9) |
| 3 | login ไม่ได้ทั้งที่สร้าง user แล้ว | dev identities (`dev_requester`, `dev_storekeeper`) ถูกปิดใช้งาน → ต้อง bootstrap ADMIN เอง |
| 4 | approve แล้วสต็อกไม่เปลี่ยน | เป็นพฤติกรรมที่ถูกต้อง — ต้องเรียก `issue` ต่อ |
| 5 | `400 INVALID_IDEMPOTENCY_KEY` | key ต้องยาว 8–100 ตัว และใช้ `A-Za-z0-9._:-` เท่านั้น |
| 6 | รายงานแสดงข้อมูลทั้งฐานข้อมูล | ถ้าไม่ส่ง `from`/`to` ระบบไม่จำกัดช่วงเวลา (ไม่มี default) |
| 7 | หน้า 2 ของ list ซ้ำ/หาย | pagination เป็น offset — อย่าเขียนข้อมูลระหว่างเปิดดูหลายหน้า |
| 8 | token เดิมใช้ไม่ได้หลังเปลี่ยนรหัส/ถูกปิดบัญชี | ระบบ revoke ทุก session โดยตั้งใจ → login ใหม่ |
| 9 | `429 TOO_MANY_ATTEMPTS` | login ผิดติดกันเกิน `AUTH_MAX_LOGIN_ATTEMPTS` (default 8 / 15 นาที) — รอหรือรีสตาร์ทแอป (limiter อยู่ใน memory) |
| 10 | `409 INSUFFICIENT_STOCK` ทั้งที่ยอดพอ | มี item ใดขาด ระบบจะไม่จ่ายเลยสักรายการ — ดู `error.details.items[]` |

> เอกสารที่เกี่ยวข้อง: `USAGE_TH.md` (API ทั้งหมด), `docs/BACKUP_RESTORE.md` (สำรอง/กู้คืน), `README.md`
