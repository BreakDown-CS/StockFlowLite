# คู่มือการใช้งาน StockFlow Lite (ภาษาไทย)

> คู่มือนี้ครอบคลุมโค้ด **Phase 1–5** ที่มีอยู่ในรีโปตอนนี้: Health check, Products, Stock Receipts, Stock Movements และ Stock Requests (ขอเบิก → อนุมัติ/ปฏิเสธ → จ่ายของ)
>
> **หมายเหตุ:** เอกสารนี้อ้างอิงจากโค้ดจริงใน `cmd/`, `internal/`, `migrations/` ถ้ามีการแก้โค้ด อย่าลืมอัปเดตเอกสารนี้ด้วย

---

## สารบัญ

- [0. ภาพรวมระบบ (อ่านก่อน)](#0-ภาพรวมระบบ-อ่านก่อน)
- [1. การรันโปรแกรม](#1-การรันโปรแกรม)
- [2. API ทั้งหมดและวิธีใช้งาน (ละเอียด)](#2-api-ทั้งหมดและวิธีใช้งาน-ละเอียด)
  - [2.0 รูปแบบ response กลาง + รหัส error](#20-รูปแบบ-response-กลาง--รหัส-error)
  - [2.1 Health / Readiness](#21-health--readiness)
  - [2.2 Products (สินค้า)](#22-products-สินค้า)
  - [2.3 Stock Receipts (รับสินค้าเข้าคลัง)](#23-stock-receipts-รับสินค้าเข้าคลัง)
  - [2.4 Stock Movements (ประวัติการเคลื่อนไหวสต็อก)](#24-stock-movements-ประวัติการเคลื่อนไหวสต็อก)
  - [2.5 Stock Requests (ขอเบิก / อนุมัติ / จ่ายของ)](#25-stock-requests-ขอเบิก--อนุมัติ--จ่ายของ)
  - [2.6 ตัวอย่างการใช้งานครบวงจร](#26-ตัวอย่างการใช้งานครบวงจร-copy-ไปรันได้เลย)
  - [2.7 ข้อจำกัดที่ต้องรู้ก่อนใช้งานจริง](#27-ข้อจำกัดที่ต้องรู้ก่อนใช้งานจริง)
- [3. Authentication & Authorization (Phase 7)](#3-authentication--authorization-phase-7)
- [ภาคผนวก: คำสั่งและไฟล์ที่ควรรู้](#ภาคผนวก-คำสั่งและไฟล์ที่ควรรู้)

---

## 0. ภาพรวมระบบ (อ่านก่อน)

StockFlow Lite คือ REST API สำหรับจัดการสต็อกสินค้าขนาดเล็ก เขียนด้วย Go + Fiber v2 + PostgreSQL (pgxpool) แบ่งชั้นแบบ Clean Architecture:

```
cmd/api            -> main.go เริ่ม HTTP server + ประกอบ dependencies
cmd/migrate        -> CLI สำหรับ apply/rollback migration
internal/domain    -> entity + กฎธุรกิจ + interface ของ repository (ไม่รู้จัก DB/HTTP)
internal/usecase   -> orchestration (product, stockmovement, stockrequest)
internal/adapter   -> HTTP handler (Fiber) + repository (PostgreSQL)
internal/infra     -> config, pgxpool, migrator
migrations/*.sql   -> schema + seed (เรียงตาม version)
pkg/response       -> response envelope กลาง
```

หลักการสำคัญที่ต้องรู้ก่อนยิง API:

1. **endpoint ของธุรกิจอยู่ใต้ `/api/v1`** ส่วน `/health` และ `/ready` อยู่ที่ root
2. **ทุก response ใช้ envelope เดียวกัน** (`success`, `message`, `data`, `meta`, `error`) ดูข้อ 2.0
3. **จำนวนสต็อก (`quantity`) เป็นค่าบวกเสมอ** ทิศทางเข้า/ออกกำหนดด้วย `movement_type`
   - `RECEIPT` = รับเข้า (บวกเข้าสต็อก)
   - `ISSUE` = จ่ายออก (ลบออกจากสต็อก)
   - `ADJUSTMENT` = ปรับปรุง (มีใน schema แต่ยังไม่มี endpoint สร้างในเฟสนี้)
4. **แก้สต็อกได้ทางเดียวผ่าน receipt / issue** ไม่มี endpoint ให้ตั้ง `current_stock` ตรง ๆ
5. **ยังไม่มีระบบ login/JWT/role** ผู้กระทำ (requester/reviewer/actor) มาจากบัญชีนักพัฒนาที่ seed ไว้ฝั่งเซิร์ฟเวอร์เท่านั้น (ดูข้อ 2.7)

### 0.1 ตารางสรุป endpoint ทั้งหมด

| # | Method | Path | ใช้ทำอะไร | สำเร็จ |
|---|--------|------|-----------|--------|
| 1 | GET | `/health` | เช็คโปรเซสยังมีชีวิต (ไม่แตะ DB) | 200 |
| 2 | GET | `/ready` | เช็คต่อ PostgreSQL ได้ | 200 / 503 |
| 3 | POST | `/api/v1/products` | สร้างสินค้าใหม่ | 201 |
| 4 | GET | `/api/v1/products` | ดูรายการสินค้า (ค้นหา/กรอง/แบ่งหน้า) | 200 |
| 5 | GET | `/api/v1/products/:id` | ดูสินค้าตาม id | 200 |
| 6 | PUT | `/api/v1/products/:id` | แก้ไขข้อมูลสินค้า (ไม่แก้สต็อก) | 200 |
| 7 | PATCH | `/api/v1/products/:id/deactivate` | ปิดการใช้งานสินค้า | 200 |
| 8 | POST | `/api/v1/stock-receipts` | รับสินค้าเข้าคลัง (+สต็อก, +movement) | 201 |
| 9 | GET | `/api/v1/stock-movements` | ดูประวัติความเคลื่อนไหวสต็อก | 200 |
| 10 | POST | `/api/v1/stock-requests` | สร้างใบขอเบิก (PENDING) | 201 |
| 11 | GET | `/api/v1/stock-requests` | ดูรายการใบขอเบิก | 200 |
| 12 | GET | `/api/v1/stock-requests/:id` | ดูใบขอเบิกตาม id (พร้อม items) | 200 |
| 13 | POST | `/api/v1/stock-requests/:id/approve` | อนุมัติ (PENDING → APPROVED) | 200 |
| 14 | POST | `/api/v1/stock-requests/:id/reject` | ปฏิเสธ (PENDING → REJECTED) | 200 |
| 15 | POST | `/api/v1/stock-requests/:id/cancel` | ยกเลิก (PENDING/APPROVED → CANCELLED, ไม่แตะสต็อก) | 200 |
| 16 | POST | `/api/v1/stock-requests/:id/issue` | จ่ายของจริง (APPROVED → ISSUED + ตัดสต็อก) รองรับ `Idempotency-Key` | 200 |
| 17 | POST | `/api/v1/auth/login` | ล็อกอิน รับ Bearer token (public) | 200 |
| 18 | POST | `/api/v1/auth/logout` | ออกจากระบบ (revoke session) | 200 |
| 19 | GET | `/api/v1/auth/me` | ข้อมูลผู้ใช้ปัจจุบัน | 200 |
| 20 | POST | `/api/v1/auth/change-password` | เปลี่ยนรหัสผ่าน (revoke ทุก session) | 200 |
| 21 | POST | `/api/v1/users` | สร้างผู้ใช้ (ADMIN) | 201 |
| 22 | GET | `/api/v1/users` | ดูรายการผู้ใช้ (ADMIN) | 200 |
| 23 | GET | `/api/v1/users/:id` | ดูผู้ใช้ (ADMIN) | 200 |
| 24 | PATCH | `/api/v1/users/:id` | เปลี่ยน role / เปิด-ปิดบัญชี (ADMIN) | 200 |
| 25 | POST | `/api/v1/users/:id/reset-password` | รีเซ็ตรหัสผ่าน (ADMIN) | 200 |
| 26 | GET | `/api/v1/audit-events` | ดู audit trail (ADMIN) | 200 |

### 0.2 โครงสร้างข้อมูลหลัก

| ตาราง | ฟิลด์สำคัญ | หมายเหตุ |
|--------|-----------|----------|
| `users` | `id`, `username`, `password_hash`, `role` (`ADMIN`/`STOREKEEPER`/`REQUESTER`) | FK ของผู้ขอ/ผู้อนุมัติ (ยังไม่มี endpoint จัดการ user) |
| `products` | `id` (UUID), `sku` (unique), `name`, `unit`, `current_stock` (INT ≥ 0), `min_stock`, `is_active` | `current_stock` เปลี่ยนได้เฉพาะผ่าน receipt/issue |
| `stock_movements` | `id` (BIGSERIAL), `product_id`, `movement_type`, `quantity` (> 0), `reference_type`, `reference_id`, `actor_id`, `notes` | ledger (immutable) ไม่มี endpoint แก้/ลบ |
| `stock_requests` | `id`, `request_number` (unique), `requester_id`, `status`, `notes`, `reviewer_id`, `reviewed_at`, `rejection_reason` | `PENDING` → `APPROVED` → `ISSUED` หรือ `PENDING` → `REJECTED` |
| `stock_request_items` | `request_id`, `product_id`, `requested_quantity` (> 0), `UNIQUE(request_id, product_id)` | ห้ามสินค้าซ้ำในใบเดียวกัน |
| `stock_request_issuances` | `request_id` (UNIQUE), `idempotency_key` (UNIQUE), `actor_id`, `item_count`, `total_quantity`, `created_at` | บันทึกการจ่ายของ (audit) + กันจ่ายซ้ำระดับฐานข้อมูล — Phase 6 |

---

## 1. การรันโปรแกรม

### 1.1 สิ่งที่ต้องเตรียม

| สิ่งที่ต้องมี | เวอร์ชัน | ตรวจสอบด้วย |
|---------------|----------|--------------|
| Go | 1.27 (ตาม `go.mod`) | `go version` |
| PostgreSQL | แนะนำ 13 ขึ้นไป (ใช้ `gen_random_uuid()` แบบ built-in) | `psql --version` |
| curl หรือ Postman | ใดก็ได้ | `curl.exe --version` (Windows 10+ มีให้อยู่แล้ว) |

ไม่ต้องใช้ Docker — ออกแบบให้รันกับ PostgreSQL ที่ติดตั้งในเครื่อง (local) เท่านั้น

Dependencies หลักมีใน `go.mod` แล้ว ไม่ต้องติดตั้งเพิ่ม: `gofiber/fiber/v2`, `jackc/pgx/v5`, `google/uuid`, `stretchr/testify`

### 1.2 สร้างฐานข้อมูล PostgreSQL

ค่าเริ่มต้นในโค้ด (`internal/config/config.go`):

```
DB_HOST=127.0.0.1   DB_PORT=5432   DB_USER=stockflow_user
DB_PASSWORD=stockflow_dev_password   DB_NAME=stockflow_db
```

สร้าง role + database ให้ตรงกับค่าเริ่มต้น (รันด้วย superuser เช่น `postgres`):

```sql
-- เข้า psql ก่อน: psql -U postgres
CREATE ROLE stockflow_user LOGIN PASSWORD 'stockflow_dev_password';
CREATE DATABASE stockflow_db OWNER stockflow_user;
```

> ถ้าเป็น PostgreSQL เวอร์ชันเก่ากว่า 13 แล้วเจอ error เรื่อง `gen_random_uuid` ให้รัน
> `CREATE EXTENSION IF NOT EXISTS pgcrypto;` ในฐานข้อมูล `stockflow_db`
>
> ใช้ค่าเริ่มต้นไม่สะดวกก็ได้ — แต่ต้องตั้ง environment variable ให้ตรง (ข้อ 1.3)
>
> 💡 ถ้าเจอ error `password authentication failed` (SQLSTATE 28P01) ให้ใช้สคริปต์ `scripts\setup_local_db.sql` ตามข้อ **1.2.1**

#### 1.2.1 ถ้าเจอ `password authentication failed for user "stockflow_user"` (SQLSTATE 28P01)

อาการนี้แปลว่า **ต่อถึง PostgreSQL ได้แล้ว** แต่ยืนยันตัวตนไม่ผ่าน สาเหตุมี 2 อย่าง:

1. ยังไม่มี role `stockflow_user` ในระบบ
2. มี role แล้ว แต่รหัสผ่านไม่ตรงกับ `DB_PASSWORD` (`ENV_FILE` → ค่าใน `.env` → ค่าเริ่มต้น `stockflow_dev_password`)

> ตั้งแต่เวอร์ชันนี้โปรแกรมโหลด **`.env`** ให้อัตโนมัติแล้ว ถ้าไฟล์ `.env` มี `DB_PASSWORD` ที่ถูกต้องก็ไม่ต้องตั้ง env เองอีก (แต่ถ้าตั้ง OS env ไว้ ค่านั้นจะชนะ `.env` เสมอ)

**วิธีแก้ที่แนะนำ — รันสคริปต์สำเร็จรูป** (idempotent, ไม่ลบข้อมูลใด ๆ)

> บนเครื่องนี้ `psql` ไม่อยู่ใน PATH ต้องเรียกด้วย path เต็ม: `C:\Program Files\PostgreSQL\18\bin\psql.exe`
> (service ที่รันอยู่ชื่อ `postgresql-x64-18`)

```bat
"C:\Program Files\PostgreSQL\18\bin\psql.exe" -U postgres -h 127.0.0.1 -p 5432 -d postgres -f scripts\setup_local_db.sql
```

สคริปต์จะ: สร้าง role (หรือรีเซ็ตรหัสผ่านถ้ามีอยู่แล้ว) → สร้าง database `stockflow_db` ถ้ายังไม่มี → ตั้งให้ `stockflow_user` เป็นเจ้าของ

**หรือพิมพ์ SQL เอง** (เข้า psql ด้วย `-U postgres` ก่อน)

```sql
-- กรณี role ยังไม่มี
CREATE ROLE stockflow_user LOGIN PASSWORD 'stockflow_dev_password';

-- กรณีมี role แล้วแต่รหัสผ่านผิด
ALTER ROLE stockflow_user WITH LOGIN PASSWORD 'stockflow_dev_password';

-- ให้ stockflow_user เป็นเจ้าของ DB (สำคัญกับ PostgreSQL 15+ เพราะ schema public จำกัดสิทธิ์)
ALTER DATABASE stockflow_db OWNER TO stockflow_user;
GRANT ALL PRIVILEGES ON DATABASE stockflow_db TO stockflow_user;
```

**ทางเลือกที่ 2 — ไม่ต้องรีเซ็ตรหัส** ถ้ารู้รหัสจริงของ `stockflow_user` อยู่แล้ว ให้ตั้ง env ให้ตรง **ในหน้าต่าง cmd เดียวกับที่จะรัน `go`**

```bat
set DB_PASSWORD=รหัสจริงของ_stockflow_user
go run ./cmd/migrate -action status
```

```powershell
$env:DB_PASSWORD="รหัสจริงของ_stockflow_user"
go run ./cmd/migrate -action status
```

**ตรวจสอบว่าต่อได้แล้ว**

```bat
"C:\Program Files\PostgreSQL\18\bin\psql.exe" "postgres://stockflow_user:stockflow_dev_password@127.0.0.1:5432/stockflow_db?sslmode=disable" -c "SELECT current_user, current_database();"
```

ถ้าคำสั่งนี้คืนผลลัพธ์ (เช่น `stockflow_user | stockflow_db`) แปลว่าพร้อมรัน `go run ./cmd/migrate -action up` ต่อได้ทันที

**กรณีไม่รู้รหัสผ่านของ `postgres` เลย**

- ใช้ **pgAdmin 4** (ถ้าติดตั้งตอนลง PostgreSQL): `Login/Group Roles` → `stockflow_user` → `Definition` → ตั้ง password ใหม่
- หรือแก้ `C:\Program Files\PostgreSQL\18\data\pg_hba.conf` ให้เป็น `trust` ชั่วคราว แล้ว restart service `postgresql-x64-18` (ต้องสิทธิ์ admin: `net stop postgresql-x64-18 && net start postgresql-x64-18`) — ⚠️ **ต้องเปลี่ยนกลับเป็น `scram-sha-256` ทันทีหลังตั้งรหัสเสร็จ**

> หมายเหตุ: environment variable แยกตามหน้าต่าง cmd/PowerShell ถ้าตั้งในหน้าต่างหนึ่งแล้วไปรันอีกหน้าต่างจะไม่มีผล

### 1.3 ตั้งค่า Environment Variables

ไฟล์ `.env.example` เป็น**ตัวอย่างค่า** ส่วนโปรแกรมจะโหลดไฟล์ **`.env`** (ถ้ามี) ให้อัตโนมัติเมื่อเริ่มทำงาน โดยเรียงลำดับความสำคัญ:

1. **OS environment variable** (เช่น `set DB_PASSWORD=...` ใน shell) — ชนะทุกอย่าง
2. ค่าในไฟล์ **`.env`** (หรือไฟล์ที่ระบุผ่าน `ENV_FILE`)
3. ค่า default ในโค้ด

วิธีใช้: คัดลอก template แล้วแก้ค่าใน `.env`

```bat
copy .env.example .env
```

> ไฟล์ `.env` ถูก gitignore ไว้แล้ว (ไม่ถูก commit) — ในทางกลับกัน `.env.example` เป็นไฟล์ที่ git track อยู่ **อย่าใส่รหัสผ่านจริงลงใน `.env.example`** เพราะจะติดไปกับประวัติ repo

| ตัวแปร | default | ความหมาย |
|--------|---------|----------|
| `APP_ENV` | `development` | ชื่อ environment (ใช้ใน log) |
| `APP_PORT` | `8080` | พอร์ต HTTP server |
| `APP_SHUTDOWN_TIMEOUT` | `10s` | เวลารอ graceful shutdown |
| `DB_HOST` | `127.0.0.1` | host ของ PostgreSQL |
| `DB_PORT` | `5432` | พอร์ต PostgreSQL |
| `DB_USER` | `stockflow_user` | user |
| `DB_PASSWORD` | `stockflow_dev_password` | รหัสผ่าน |
| `DB_NAME` | `stockflow_db` | ชื่อฐานข้อมูล |
| `DB_SSLMODE` | `disable` | `disable` / `require` ฯลฯ |
| `DB_MAX_CONNS` | `25` | จำนวน connection สูงสุดใน pool |
| `DB_MIN_CONNS` | `5` | จำนวน connection ขั้นต่ำ |
| `DB_MAX_CONN_LIFETIME` | `1h` | อายุ connection สูงสุด |
| `DB_MAX_CONN_IDLE_TIME` | `30m` | เวลาทิ้ง connection ที่ว่าง |
| `DB_HEALTH_TIMEOUT` | `3s` | timeout ของ ping ใน `/ready` |
| `AUTH_TOKEN_TTL` | `12h` | อายุของ Bearer token (session) |
| `AUTH_TOKEN_PEPPER` | `stockflow-development-pepper-change-me` | **ความลับ** สำหรับ hash token — ต้อง ≥ 16 ตัว และต้องเปลี่ยนก่อนใช้จริง |
| `AUTH_PBKDF2_ITERATIONS` | `600000` | จำนวนรอบ PBKDF2 (ขั้นต่ำ 100000) |
| `AUTH_MAX_LOGIN_ATTEMPTS` | `8` | จำนวนครั้งที่ล็อกอินผิดได้ก่อนถูกบล็อก |
| `AUTH_LOGIN_WINDOW` | `15m` | ช่วงเวลาที่นับ/บล็อก |
| `ENV_FILE` | `.env` | ระบุไฟล์ env อื่น (เช่น `.env.local`) |

**Windows – cmd.exe**
```bat
set DB_HOST=127.0.0.1
set DB_PORT=5432
set DB_USER=stockflow_user
set DB_PASSWORD=stockflow_dev_password
set DB_NAME=stockflow_db
set APP_PORT=8080
```

**Windows – PowerShell**
```powershell
$env:DB_HOST="127.0.0.1"
$env:DB_PORT="5432"
$env:DB_USER="stockflow_user"
$env:DB_PASSWORD="stockflow_dev_password"
$env:DB_NAME="stockflow_db"
$env:APP_PORT="8080"
```

**Linux / macOS**
```bash
export DB_HOST=127.0.0.1 DB_PORT=5432
export DB_USER=stockflow_user DB_PASSWORD=stockflow_dev_password DB_NAME=stockflow_db
export APP_PORT=8080
```

> ไม่ตั้งอะไรเลยก็ได้ ถ้าสร้าง DB ตามข้อ 1.2 (จะใช้ค่า default ทั้งหมด)

### 1.4 รัน Migration

migration เป็นไฟล์ SQL แบบ versioned ใน `migrations/` และมีตาราง `schema_migrations` เก็บว่าเวอร์ชันไหนรันแล้ว

| ไฟล์ | สร้างอะไร |
|------|-----------|
| `000001_create_users` | ตาราง `users` |
| `000002_create_products` | ตาราง `products` + index |
| `000003_create_stock_requests` | `stock_requests`, `stock_request_items` |
| `000004_create_stock_movements` | `stock_movements` (ledger) |
| `000005_seed_development_identities` | seed user dev 2 คน ใช้เป็นผู้ขอ/ผู้อนุมัติ |
| `000006_create_stock_request_issuances` | ตารางบันทึกการจ่ายของ (audit) + `Idempotency-Key` (กันจ่ายซ้ำ) |

คำสั่ง (รันจาก root ของโปรเจกต์):

```bash
# ดูสถานะว่าฐานข้อมูลมีอะไรแล้ว/ขาดอะไร
go run ./cmd/migrate -action status

# apply migration ทั้งหมดที่ยังไม่รัน
go run ./cmd/migrate -action up

# apply ทีละ 1 เวอร์ชัน
go run ./cmd/migrate -action up -steps 1

# rollback 1 เวอร์ชันล่าสุด (ย้อน schema จริง ระวัง)
go run ./cmd/migrate -action down
```

ตัวเลือก: `-action up|down|status`, `-steps N` (`up` = 0 คือทั้งหมด, `down` default = 1), `-dir migrations`

log ที่สำเร็จหน้าตาประมาณนี้:

```
time=... level=INFO msg="Connecting to database" target=postgres://stockflow_user:***@127.0.0.1:5432/stockflow_db?sslmode=disable
time=... level=INFO msg="Applied migration successfully" migration="000005 (seed_development_identities)"
```

> **ต้องรันให้ครบทุกไฟล์ก่อนใช้งาน API** ถ้าขาด `000005` การสร้างใบขอเบิกจะล้มเหลว เพราะ `requester_id` เป็น FK ไป `users.id`

### 1.5 รัน API Server

```bash
go run ./cmd/api
```

log ที่ควรเห็นตอนเริ่ม:

```json
{"time":"...","level":"INFO","msg":"Loaded environment file","file":".env"}
{"time":"...","level":"INFO","msg":"Starting StockFlow Lite API","env":"development","port":8080,"db_target":"postgres://stockflow_user:***@127.0.0.1:5432/stockflow_db?sslmode=disable"}
{"time":"...","level":"INFO","msg":"Successfully established connection to PostgreSQL"}
{"time":"...","level":"INFO","msg":"HTTP server listening","address":":8080"}
```

ตรวจว่าใช้งานได้:

```bash
curl.exe http://localhost:8080/health
# {"status":"ok","service":"stockflow-lite"}

curl.exe http://localhost:8080/ready
# {"status":"ready","database":"up"}
```

> ถ้า `/health` ตอบ 200 แต่ `/ready` ตอบ 503 แปลว่า server ทำงานแต่ต่อ DB ไม่ได้ (ค่า DB ผิด หรือ DB ยังไม่ start)

หยุด server: `Ctrl+C` (จะปิดแบบ graceful ตาม `APP_SHUTDOWN_TIMEOUT`)

### 1.6 รันชุดทดสอบ (go test / go vet)

```bash
go vet ./...
go test ./...
```

integration test ที่ต้องใช้ PostgreSQL (`internal/adapter/repository/postgres`) จะ **skip อัตโนมัติ** ถ้าต่อฐานข้อมูลไม่ได้ พร้อมข้อความ

```
--- SKIP: TestStockMovementRepository_ReceiveStock_Success
    skipping PostgreSQL integration test: database unreachable ...
```

ถ้าต้องการให้ integration test รันจริง ให้ชี้ไปที่ฐานข้อมูล dev/test ที่แยกไว้:

```bat
:: Windows cmd
set TEST_DATABASE_URL=postgres://stockflow_user:stockflow_dev_password@127.0.0.1:5432/stockflow_db?sslmode=disable
```

```bash
# Linux / macOS
export TEST_DATABASE_URL="postgres://stockflow_user:stockflow_dev_password@127.0.0.1:5432/stockflow_db?sslmode=disable"

go test ./... -count=1
```

> ⚠️ integration test จะสร้างและลบข้อมูลของตัวเอง (สินค้า/ใบขอ/movement ที่สร้างในเทสต์) ไม่แตะข้อมูลอื่น แต่แนะนำให้ชี้ไปที่ฐานข้อมูลสำหรับ test เท่านั้น

### 1.7 แก้ปัญหาที่พบบ่อย

| อาการ | สาเหตุ | วิธีแก้ |
|-------|---------|--------|
| `connectex: No connection could be made ... 5432` | PostgreSQL ไม่ได้รัน หรือ host/port ผิด | เปิด service PostgreSQL, เช็ค `DB_HOST`/`DB_PORT` |
| `password authentication failed for user "stockflow_user"` | รหัสผ่านใน `.env` / OS env ไม่ตรงกับใน PostgreSQL | ดูข้อ **1.2.1** (รัน `scripts\setup_local_db.sql`) |
| `database "stockflow_db" does not exist` | ยังไม่สร้าง DB | รัน SQL ข้อ 1.2 |
| `relation "products" does not exist` | ยังไม่รัน migration | `go run ./cmd/migrate -action up` |
| สร้างใบขอเบิกแล้วได้ 500 และ log มี FK `requester_id` | ยังไม่รัน migration `000005` | `go run ./cmd/migrate -action up` |
| `listen tcp :8080: bind: address already in use` | พอร์ตถูกใช้อยู่ | เปลี่ยน `APP_PORT` หรือปิดโปรเซสเดิม |
| `function gen_random_uuid() does not exist` | PostgreSQL < 13 | `CREATE EXTENSION IF NOT EXISTS pgcrypto;` |
| รัน `go test` แล้ว integration test ขึ้น SKIP | ไม่มี DB ในเครื่อง | ปกติ (ไม่ใช่ error) หรือตั้ง `TEST_DATABASE_URL` |

---

## 2. API ทั้งหมดและวิธีใช้งาน (ละเอียด)

### 2.0 รูปแบบ response กลาง + รหัส error

**Base URL:** `http://localhost:8080` (เปลี่ยนตาม `APP_PORT`)
**Content-Type:** `application/json` (ทุก request ที่มี body)

#### รูปแบบสำเร็จ

```json
{
  "success": true,
  "message": "ข้อความอธิบาย (อาจไม่มี)",
  "data": { },
  "meta": { "limit": 20, "offset": 0, "total": 42 }
}
```

- `data` = ข้อมูลจริง (object หรือ array)
- `meta` = มีเฉพาะ endpoint แบบ list (ใช้ทำ pagination)
- `message` = มีใน endpoint ที่มีการเปลี่ยนข้อมูล

#### รูปแบบผิดพลาด

```json
{
  "success": false,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "quantity must be strictly positive"
  }
}
```

> บางกรณีมี `error.details` เพิ่มเติมเพื่อให้ client แก้ปัญหาได้เอง เช่น
> `details.items` (รายการที่สต็อกขาด) และ `details.current_status` / `details.issuance_id` (สถานะ/การจ่ายของใบขอ)

#### HTTP status ที่ใช้

| Status | ใช้เมื่อ |
|--------|----------|
| `200 OK` | อ่านข้อมูลสำเร็จ / เปลี่ยนสถานะสำเร็จ |
| `201 Created` | สร้างข้อมูลใหม่สำเร็จ (product, receipt, stock request) |
| `400 Bad Request` | body ผิดรูปแบบ, ค่าที่ส่งมาไม่ผ่าน validation, id ผิดรูปแบบ, query parameter ผิด |
| `404 Not Found` | ไม่พบสินค้า / ไม่พบใบขอเบิก |
| `409 Conflict` | สถานะไม่ถูกต้อง (อนุมัติซ้ำ/จ่ายซ้ำ), สต็อกไม่พอ, SKU ซ้ำ, ใบขอซ้ำ |
| `500 Internal Server Error` | ข้อผิดพลาดที่ไม่คาดคิด (ไม่ส่งรายละเอียด SQL/credential ออกไป) |
| `503 Service Unavailable` | `/ready` ต่อฐานข้อมูลไม่ได้ |

#### รหัส error (`error.code`) ที่อาจเจอ

| code | ความหมาย | ตัวอย่างสาเหตุ |
|------|----------|----------------|
| `BAD_REQUEST` | JSON ผิดรูปแบบ/parse ไม่ได้ | ส่ง body ที่ไม่ใช่ JSON |
| `VALIDATION_ERROR` | ค่าไม่ผ่านกฎ | quantity ≤ 0, ไม่ส่ง name, notes ยาวเกิน |
| `INVALID_ID` | UUID ผิดรูปแบบ | `/products/abc` |
| `INVALID_PARAMETER` | query string ผิด | `limit=0`, `status=DELIVERED` |
| `NOT_FOUND` | ไม่พบ resource | id ที่ไม่มีในฐานข้อมูล |
| `DUPLICATE_SKU` | SKU ซ้ำ | สร้างสินค้าด้วย SKU เดิม |
| `DUPLICATE_ITEM` | สินค้าซ้ำในใบขอเดียว | items มี product_id ซ้ำกัน |
| `DUPLICATE_REQUEST_NUMBER` | เลขที่ใบขอซ้ำ (แทบไม่เกิด) | ชนกันของ request_number |
| `PRODUCT_INACTIVE` | สินค้าถูกปิดการใช้งาน | รับเข้า/จ่ายของกับสินค้า `is_active=false` |
| `INVALID_STATE` | สถานะใบขอไม่ถูกต้อง | อนุมัติใบที่ APPROVED แล้ว, จ่ายใบที่ยัง PENDING, ยกเลิกใบที่จ่ายแล้ว |
| `INSUFFICIENT_STOCK` | สต็อกไม่พอสำหรับจ่าย | ขอ 10 แต่มี 3 (มี `details.items` บอกทีละรายการ) |
| `INVALID_IDEMPOTENCY_KEY` | `Idempotency-Key` ผิดรูปแบบ | สั้นกว่า 8 ตัว / ยาวเกิน 100 / มีอักขระต้องห้าม |
| `IDEMPOTENCY_KEY_REUSED` | key ถูกใช้กับใบขออื่นแล้ว | ใช้ key เดิมยิงคนละใบ |
| `STOCK_OVERFLOW` | รับเข้าจนเกินขอบเขต INT | current_stock ใกล้ 2,147,483,647 |
| `INTERNAL_ERROR` | เซิร์ฟเวอร์ผิดพลาด | DB ล่ม, constraint ที่ไม่รู้จัก |

#### ข้อตกลงทั่วไป

- เวลา (`created_at`, `updated_at`, `reviewed_at`) เป็น ISO-8601 แบบ UTC เช่น `2026-10-28T09:15:19.123456Z`
- `id` ของสินค้า/ใบขอเป็น **UUID** ส่วน `id` ของ movement เป็น **จำนวนเต็ม** (BIGSERIAL)
- ค่า `quantity` และ `requested_quantity` **ต้องเป็นจำนวนเต็มบวก** เท่านั้น
- endpoint ที่อยู่ใต้ `/api/v1/stock-requests` รับ path ทั้งมีและไม่มี `/` ปิดท้าย (`/stock-requests` = `/stock-requests/`)
- client **ไม่สามารถ** ตั้ง `requester_id`, `reviewer_id`, `actor_id`, `current_stock` ได้ ถ้าส่งมาใน body ระบบจะไม่สนใจ (ถูก ignore)

> **หมายเหตุสำหรับ Windows:** ใน PowerShell คำสั่ง `curl` อาจถูก alias ไป `Invoke-WebRequest` ให้ใช้ `curl.exe` ตามตัวอย่าง หรือใช้ `Invoke-RestMethod` (ดูตัวอย่างในข้อ 2.6)

### 2.1 Health / Readiness

endpoint กลุ่มนี้ **ไม่ใช้ envelope** กลาง (ตอบ JSON ตรง ๆ) และไม่ต้องมี body

#### GET `/health` — liveness

ใช้เช็คว่าโปรเซสยังทำงานอยู่ ไม่ตรวจฐานข้อมูล

```bash
curl.exe http://localhost:8080/health
```

```json
{ "status": "ok", "service": "stockflow-lite" }
```

| Status | ความหมาย |
|--------|----------|
| 200 | โปรเซสยังทำงาน |

#### GET `/ready` — readiness

ตรวจว่าต่อ PostgreSQL ได้จริง (ping แบบมี timeout ตาม `DB_HEALTH_TIMEOUT`)

```bash
curl.exe http://localhost:8080/ready
```

กรณีปกติ (200):
```json
{ "status": "ready", "database": "up" }
```

กรณี DB ล่ม (503):
```json
{ "status": "unavailable", "database": "down" }
```

### 2.2 Products (สินค้า)

#### 2.2.1 สร้างสินค้า — `POST /api/v1/products`

**Body**

| field | type | จำเป็น | กฎ |
|-------|------|---------|-----|
| `sku` | string | ✅ | ห้ามว่าง, ยาว ≤ 50, **ห้ามซ้ำ** |
| `name` | string | ✅ | ห้ามว่าง, ยาว ≤ 200 |
| `description` | string | ❌ | ยาว ≤ 1000 |
| `unit` | string | ❌ | ค่าเริ่มต้น `"PCS"`, ยาว ≤ 20 |
| `min_stock` | integer | ❌ | ≥ 0, ค่าเริ่มต้น `0` |

สินค้าที่สร้างใหม่จะมี `current_stock = 0` และ `is_active = true` เสมอ

```bash
curl.exe -X POST http://localhost:8080/api/v1/products ^
  -H "Content-Type: application/json" ^
  -d "{\"sku\":\"SKU-1001\",\"name\":\"ปากกาลูกลื่นสีน้ำเงิน\",\"description\":\"กล่อง 50 ด้าม\",\"unit\":\"BOX\",\"min_stock\":10}"
```

> ถ้าใช้ PowerShell ให้เปลี่ยน `^` เป็น backtick `` ` `` หรือเขียนในบรรทัดเดียว

**สำเร็จ (201)**

```json
{
  "success": true,
  "message": "Product created successfully",
  "data": {
    "id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
    "sku": "SKU-1001",
    "name": "ปากกาลูกลื่นสีน้ำเงิน",
    "description": "กล่อง 50 ด้าม",
    "unit": "BOX",
    "current_stock": 0,
    "min_stock": 10,
    "is_active": true,
    "created_at": "2026-10-28T09:00:00.123456Z",
    "updated_at": "2026-10-28T09:00:00.123456Z"
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `BAD_REQUEST` | body ไม่ใช่ JSON ที่ถูกต้อง |
| 400 | `VALIDATION_ERROR` | sku/name ว่าง, ยาวเกิน, min_stock ติดลบ |
| 409 | `DUPLICATE_SKU` | SKU ซ้ำกับที่มีอยู่ |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด |

```json
{
  "success": false,
  "error": { "code": "DUPLICATE_SKU", "message": "A product with this SKU already exists" }
}
```

#### 2.2.2 ดูรายการสินค้า — `GET /api/v1/products`

**Query parameters**

| param | type | ค่าเริ่มต้น | ความหมาย |
|-------|------|-------------|----------|
| `limit` | int > 0 | 20 | จำนวนต่อหน้า (ระบบจำกัดไม่เกิน 100) |
| `offset` | int ≥ 0 | 0 | ข้ามกี่รายการ |
| `search` | string | – | ค้นหาใน `sku` หรือ `name` (แบบบางส่วน ไม่สนตัวพิมพ์เล็ก/ใหญ่) |
| `is_active` | `true` / `false` / `all` | `true` | **ค่าเริ่มต้นแสดงเฉพาะสินค้าที่ใช้งานอยู่** |

```bash
# สินค้าที่ใช้งานอยู่ 20 รายการแรก
curl.exe "http://localhost:8080/api/v1/products?limit=20&offset=0"

# ค้นหา + รวมสินค้าที่ปิดใช้งานด้วย
curl.exe "http://localhost:8080/api/v1/products?search=pen&is_active=all"
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "data": [
    {
      "id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
      "sku": "SKU-1001",
      "name": "ปากกาลูกลื่นสีน้ำเงิน",
      "description": "กล่อง 50 ด้าม",
      "unit": "BOX",
      "current_stock": 40,
      "min_stock": 10,
      "is_active": true,
      "created_at": "2026-10-28T09:00:00.123456Z",
      "updated_at": "2026-10-28T09:10:00.654321Z"
    }
  ],
  "meta": { "limit": 20, "offset": 0, "total": 1 }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `INVALID_PARAMETER` | `limit` ≤ 0 หรือไม่ใช่ตัวเลข, `offset` ติดลบ, `is_active` ไม่ใช่ true/false/all |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด |

> `meta.total` คือจำนวนทั้งหมดที่ตรงเงื่อนไข (ไม่ใช่จำนวนในหน้านี้) ใช้คำนวณจำนวนหน้าได้: `ceil(total / limit)`

#### 2.2.3 ดูสินค้าตาม id — `GET /api/v1/products/:id`

```bash
curl.exe http://localhost:8080/api/v1/products/3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "data": {
    "id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
    "sku": "SKU-1001",
    "name": "ปากกาลูกลื่นสีน้ำเงิน",
    "description": "กล่อง 50 ด้าม",
    "unit": "BOX",
    "current_stock": 40,
    "min_stock": 10,
    "is_active": true,
    "created_at": "2026-10-28T09:00:00.123456Z",
    "updated_at": "2026-10-28T09:10:00.654321Z"
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `INVALID_ID` | `:id` ไม่ใช่ UUID |
| 404 | `NOT_FOUND` | ไม่พบสินค้า |

```json
{ "success": false, "error": { "code": "NOT_FOUND", "message": "Product not found" } }
```

#### 2.2.4 แก้ไขสินค้า — `PUT /api/v1/products/:id`

**Body**

| field | type | จำเป็น | กฎ / พฤติกรรม |
|-------|------|---------|----------------|
| `name` | string | ✅ | ห้ามว่าง, ≤ 200 |
| `description` | string | ❌ | ≤ 1000 (ไม่ส่ง = ล้างเป็นค่าว่าง) |
| `unit` | string | ❌ | ≤ 20 — **ถ้าไม่ส่ง/ส่งค่าว่าง จะคงค่าเดิมไว้** |
| `min_stock` | integer | ❌ | ≥ 0 — **ถ้าไม่ส่ง จะถูกตั้งเป็น 0** |

> ⚠️ **ข้อควรระวัง:** endpoint นี้อัปเดตแบบ "แทนที่ทั้งก้อน" (`min_stock` ที่ไม่ส่งจะกลายเป็น 0) ถ้าต้องการคงค่าเดิม ต้องส่งค่าเดิมกลับมาด้วย
> ⚠️ `sku` และ `current_stock` **แก้ผ่าน endpoint นี้ไม่ได้** (ถ้าส่งมาจะถูก ignore) — สต็อกแก้ได้เฉพาะ receipt/issue

```bash
curl.exe -X PUT http://localhost:8080/api/v1/products/3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60 ^
  -H "Content-Type: application/json" ^
  -d "{\"name\":\"ปากกาลูกลื่นสีน้ำเงิน (แพ็คใหม่)\",\"description\":\"กล่อง 100 ด้าม\",\"unit\":\"BOX\",\"min_stock\":15}"
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "message": "Product updated successfully",
  "data": {
    "id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
    "sku": "SKU-1001",
    "name": "ปากกาลูกลื่นสีน้ำเงิน (แพ็คใหม่)",
    "description": "กล่อง 100 ด้าม",
    "unit": "BOX",
    "current_stock": 40,
    "min_stock": 15,
    "is_active": true,
    "created_at": "2026-10-28T09:00:00.123456Z",
    "updated_at": "2026-10-28T09:20:11.111111Z"
  }
}
```

**ข้อผิดพลาด**: 400 `INVALID_ID` / `BAD_REQUEST` / `VALIDATION_ERROR`, 404 `NOT_FOUND`

#### 2.2.5 ปิดการใช้งานสินค้า — `PATCH /api/v1/products/:id/deactivate`

ไม่ต้องส่ง body เป็นการตั้ง `is_active = false` (เรียกซ้ำได้ ไม่ error — idempotent)

```bash
curl.exe -X PATCH http://localhost:8080/api/v1/products/3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60/deactivate
```

**สำเร็จ (200)** — ไม่มี `data` เพราะไม่ได้คืนข้อมูล

```json
{
  "success": true,
  "message": "Product deactivated successfully"
}
```

**ข้อผิดพลาด**: 400 `INVALID_ID`, 404 `NOT_FOUND`

> สินค้าที่ปิดใช้งานแล้ว: ยังดูรายการได้ (`is_active=false` หรือ `all`) แต่ **รับเข้า/ขอเบิก/จ่ายของไม่ได้** และจะถูกซ่อนจาก list ที่ใช้ค่าเริ่มต้น

### 2.3 Stock Receipts (รับสินค้าเข้าคลัง)

#### POST `/api/v1/stock-receipts`

ใช้เมื่อ "ของเข้ามาในคลัง" (ซื้อเข้า/รับจาก supplier) ระบบจะ **เพิ่ม `current_stock` และบันทึก movement ชนิด `RECEIPT`** ในทรานแซกชันเดียว (สำเร็จทั้งคู่ หรือไม่เกิดอะไรเลย)

**Body**

| field | type | จำเป็น | กฎ |
|-------|------|---------|-----|
| `product_id` | UUID string | ✅ | ต้องมีอยู่จริงและ `is_active = true` |
| `quantity` | integer | ✅ | ต้อง **> 0** (ศูนย์/ติดลบไม่ได้) |
| `notes` | string | ❌ | ≤ 1000 ตัวอักษร |

> client **ไม่สามารถ** กำหนด `current_stock` ใหม่เองได้ (ถ้าส่งมาใน body จะถูก ignore) ระบบจะบวกเพิ่มจากค่าเดิมเสมอ

```bash
curl.exe -X POST http://localhost:8080/api/v1/stock-receipts ^
  -H "Content-Type: application/json" ^
  -d "{\"product_id\":\"3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60\",\"quantity\":40,\"notes\":\"รับจาก Supplier A\"}"
```

**สำเร็จ (201)**

```json
{
  "success": true,
  "message": "Stock received successfully",
  "data": {
    "product": {
      "id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
      "sku": "SKU-1001",
      "name": "ปากกาลูกลื่นสีน้ำเงิน",
      "unit": "BOX",
      "current_stock": 40,
      "min_stock": 10,
      "is_active": true,
      "created_at": "2026-10-28T09:00:00.123456Z",
      "updated_at": "2026-10-28T09:10:00.654321Z"
    },
    "movement": {
      "id": 12,
      "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
      "movement_type": "RECEIPT",
      "quantity": 40,
      "reference_type": "STOCK_RECEIPT",
      "notes": "รับจาก Supplier A",
      "created_at": "2026-10-28T09:10:00.654321Z"
    }
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `BAD_REQUEST` | body ไม่ใช่ JSON |
| 400 | `VALIDATION_ERROR` | ไม่ส่ง `quantity` หรือ `quantity ≤ 0` |
| 400 | `INVALID_ID` | `product_id` ไม่ใช่ UUID |
| 400 | `PRODUCT_INACTIVE` | สินค้าถูกปิดการใช้งาน |
| 400 | `STOCK_OVERFLOW` | รับเข้าแล้วสต็อกเกินขีดจำกัด INT |
| 404 | `NOT_FOUND` | ไม่พบสินค้า |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด (ทั้ง transaction ถูก rollback) |

```json
{
  "success": false,
  "error": { "code": "PRODUCT_INACTIVE", "message": "Cannot receive stock for inactive product" }
}
```

> **Concurrency:** ถ้ามีหลาย request รับเข้าสินค้าเดียวกันพร้อมกัน ระบบล็อกแถวสินค้า (`SELECT … FOR UPDATE`) และบวกด้วย SQL โดยตรง ทำให้ยอดไม่หาย (no lost update)

### 2.4 Stock Movements (ประวัติการเคลื่อนไหวสต็อก)

#### GET `/api/v1/stock-movements`

ประวัติ ledger แบบอ่านอย่างเดียว (ไม่มี endpoint แก้/ลบ) เรียงจากใหม่ไปเก่า (`created_at DESC, id DESC`)

**Query parameters**

| param | type | ค่าเริ่มต้น | ความหมาย |
|-------|------|-------------|----------|
| `limit` | int > 0 | 20 | จำนวนต่อหน้า (สูงสุด 100) |
| `offset` | int ≥ 0 | 0 | ข้ามกี่รายการ |
| `product_id` | UUID | – | กรองเฉพาะสินค้า |
| `movement_type` | `RECEIPT` / `ISSUE` / `ADJUSTMENT` | – | กรองตามชนิด (พิมพ์เล็ก/ใหญ่ได้) |

```bash
# 10 รายการล่าสุดของสินค้าหนึ่ง
curl.exe "http://localhost:8080/api/v1/stock-movements?limit=10&offset=0&product_id=3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60"

# เฉพาะการจ่ายออก
curl.exe "http://localhost:8080/api/v1/stock-movements?movement_type=ISSUE"
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "data": [
    {
      "id": 21,
      "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
      "movement_type": "ISSUE",
      "quantity": 5,
      "reference_type": "STOCK_REQUEST",
      "reference_id": "REQ-20261028-9F3A1B2C",
      "actor_id": "00000000-0000-4000-8000-000000000002",
      "created_at": "2026-10-28T10:05:00.000000Z"
    },
    {
      "id": 12,
      "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
      "movement_type": "RECEIPT",
      "quantity": 40,
      "reference_type": "STOCK_RECEIPT",
      "notes": "รับจาก Supplier A",
      "created_at": "2026-10-28T09:10:00.654321Z"
    }
  ],
  "meta": { "limit": 10, "offset": 0, "total": 2 }
}
```

**การอ่านค่า `quantity`**

| movement_type | ความหมายต่อสต็อก | ตัวอย่าง |
|---------------|-------------------|----------|
| `RECEIPT` | **+quantity** | quantity 40 = ของเข้า 40 |
| `ISSUE` | **-quantity** | quantity 5 = ของออก 5 |
| `ADJUSTMENT` | +quantity (ตามที่มีใน domain) | ยังไม่มี endpoint สร้าง |

> `reference_id` และ `notes`/`actor_id` จะหายไปจาก JSON ถ้าเป็นค่าว่าง/NULL (เพราะเป็น `omitempty`)

**ข้อผิดพลาด**: 400 `INVALID_PARAMETER` (limit/offset/product_id/movement_type ผิดรูปแบบ)

### 2.5 Stock Requests (ขอเบิก / อนุมัติ / จ่ายของ)

ชุดนี้คือ workflow หลักของระบบ: **ขอเบิก → อนุมัติ → จ่ายของจริง** (หรือปฏิเสธ)

#### 2.5.0 สถานะและลำดับการทำงาน (อ่านก่อน)

```
                  ┌──── reject ────► REJECTED    (จบ, ไม่แตะสต็อก)
                  │
                  ├──── cancel ────► CANCELLED   (จบ, ไม่แตะสต็อก)
PENDING ──────────┤
(สร้างใบขอ)        │
                  └──── approve ───► APPROVED ───┬──── cancel ────► CANCELLED
                                                 │      (ยกเลิกก่อนจ่ายได้)
                                                 └──── issue ─────► ISSUED
                                                        (ตัดสต็อก + สร้าง ISSUE movement)
```

| สถานะ | เกิดจาก | ผลต่อสต็อก |
|-------|---------|-------------|
| `PENDING` | POST /stock-requests | ไม่เปลี่ยน |
| `APPROVED` | POST /:id/approve | **ไม่เปลี่ยน** (อนุมัติอย่างเดียว) |
| `ISSUED` | POST /:id/issue | ตัดสต็อกทุก item + สร้าง movement `ISSUE` (atomic) |
| `REJECTED` | POST /:id/reject | ไม่เปลี่ยน |
| `CANCELLED` | POST /:id/cancel (จาก `PENDING` หรือ `APPROVED`) | ไม่เปลี่ยน |

> **ทำไม approve กับ issue แยกกัน?** เพราะ schema ออกแบบให้มี 2 สถานะ (`APPROVED`, `ISSUED`) และมี `reviewer_id`/`reviewed_at` สำหรับการอนุมัติ โดยการตัดสต็อกเกิดตอน `issue` เท่านั้น
> ถ้าต้องการอนุมัติและจ่ายในขั้นตอนเดียว ให้เรียก `approve` ต่อด้วย `issue` ทันที
>
> ทั้ง `approve` และ `issue` ใช้แถว request lock + เงื่อนไขสถานะใน SQL ทำให้ **กดซ้ำ/ยิงพร้อมกันแล้วจ่ายของซ้ำไม่ได้**

ผู้ขอ (`requester_id`) และผู้อนุมัติ (`reviewer_id`/`actor_id`) มาจากบัญชีที่ระบบ seed ไว้ (ยังไม่มี auth) — ถ้าส่งมาใน body จะถูก ignore (ดูข้อ 2.7)

#### 2.5.1 สร้างใบขอเบิก — `POST /api/v1/stock-requests`

**Body**

| field | type | จำเป็น | กฎ |
|-------|------|---------|-----|
| `items` | array | ✅ | อย่างน้อย 1 รายการ, สูงสุด 100 รายการ |
| `items[].product_id` | UUID | ✅ | ต้องมีสินค้าจริงและ `is_active = true` |
| `items[].quantity` | integer | ✅ | ต้อง **> 0** |
| `notes` | string | ❌ | ≤ 1000 ตัวอักษร |

- **ห้ามใส่สินค้าซ้ำกันในใบเดียว** (ถ้าซ้ำจะได้ `DUPLICATE_ITEM`)
- สร้างเสร็จสถานะเป็น `PENDING` และ **ยังไม่ตัดสต็อก**
- `request_number` ระบบสร้างให้อัตโนมัติรูปแบบ `REQ-YYYYMMDD-XXXXXXXX`

```bash
curl.exe -X POST http://localhost:8080/api/v1/stock-requests ^
  -H "Content-Type: application/json" ^
  -d "{\"items\":[{\"product_id\":\"3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60\",\"quantity\":5}],\"notes\":\"ขอใช้แผนกบัญชี\"}"
```

**สำเร็จ (201)**

```json
{
  "success": true,
  "message": "Stock request created successfully",
  "data": {
    "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
    "request_number": "REQ-20261028-9F3A1B2C",
    "requester_id": "00000000-0000-4000-8000-000000000001",
    "status": "PENDING",
    "notes": "ขอใช้แผนกบัญชี",
    "created_at": "2026-10-28T09:40:00.111111Z",
    "updated_at": "2026-10-28T09:40:00.111111Z",
    "items": [
      {
        "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
        "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
        "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
        "requested_quantity": 5
      }
    ]
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `BAD_REQUEST` | body ไม่ใช่ JSON |
| 400 | `VALIDATION_ERROR` | `items` ว่าง/เกิน 100, ไม่ส่ง product_id/quantity, quantity ≤ 0, notes ยาวเกิน |
| 400 | `INVALID_ID` | `product_id` ไม่ใช่ UUID |
| 400 | `DUPLICATE_ITEM` | สินค้าซ้ำในใบเดียว |
| 400 | `PRODUCT_INACTIVE` | อ้างถึงสินค้าที่ปิดใช้งาน |
| 404 | `NOT_FOUND` | อ้างถึงสินค้าที่ไม่มีอยู่จริง |
| 409 | `DUPLICATE_REQUEST_NUMBER` | เลขที่ใบขอชนกัน (แทบไม่เกิด) |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด (rollback ทั้งใบ) |

```json
{
  "success": false,
  "error": { "code": "DUPLICATE_ITEM", "message": "Duplicate product in stock request items" }
}
```

#### 2.5.2 ดูรายการใบขอเบิก — `GET /api/v1/stock-requests`

**Query parameters**

| param | type | ค่าเริ่มต้น | ความหมาย |
|-------|------|-------------|----------|
| `limit` | int > 0 | 20 | จำนวนต่อหน้า (สูงสุด 100 — ถ้าเกินระบบตัดให้เหลือ 100) |
| `offset` | int ≥ 0 | 0 | ข้ามกี่รายการ |
| `status` | `PENDING` / `APPROVED` / `REJECTED` / `ISSUED` / `CANCELLED` | – | กรองตามสถานะ (พิมพ์เล็ก/ใหญ่ได้) |

```bash
# ใบขอที่ยังรออนุมัติ
curl.exe "http://localhost:8080/api/v1/stock-requests?status=PENDING&limit=20&offset=0"
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "data": [
    {
      "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
      "request_number": "REQ-20261028-9F3A1B2C",
      "requester_id": "00000000-0000-4000-8000-000000000001",
      "status": "PENDING",
      "notes": "ขอใช้แผนกบัญชี",
      "created_at": "2026-10-28T09:40:00.111111Z",
      "updated_at": "2026-10-28T09:40:00.111111Z",
      "items": [
        {
          "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
          "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
          "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
          "requested_quantity": 5
        }
      ]
    }
  ],
  "meta": { "limit": 20, "offset": 0, "total": 1 }
}
```

เรียงจากใหม่ไปเก่า (`created_at DESC, id DESC`) และ **มี `items` ติดมาด้วยทุกใบ**

**ข้อผิดพลาด**: 400 `INVALID_PARAMETER` (`limit`/`offset` ผิด หรือ `status` ไม่ใช่ค่าที่อนุญาต)

```json
{
  "success": false,
  "error": { "code": "INVALID_PARAMETER", "message": "status must be PENDING, APPROVED, REJECTED, ISSUED, or CANCELLED" }
}
```

#### 2.5.3 ดูใบขอเบิกตาม id — `GET /api/v1/stock-requests/:id`

```bash
curl.exe http://localhost:8080/api/v1/stock-requests/b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f
```

**สำเร็จ (200)** — โครงสร้างเหมือน object ในข้อ 2.5.2 (มี `items`)

```json
{
  "success": true,
  "data": {
    "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
    "request_number": "REQ-20261028-9F3A1B2C",
    "requester_id": "00000000-0000-4000-8000-000000000001",
    "status": "PENDING",
    "notes": "ขอใช้แผนกบัญชี",
    "created_at": "2026-10-28T09:40:00.111111Z",
    "updated_at": "2026-10-28T09:40:00.111111Z",
    "items": [
      {
        "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
        "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
        "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
        "requested_quantity": 5
      }
    ]
  }
}
```

**ข้อผิดพลาด**: 400 `INVALID_ID`, 404 `NOT_FOUND`

#### 2.5.4 อนุมัติใบขอเบิก — `POST /api/v1/stock-requests/:id/approve`

ไม่ต้องส่ง body (ถ้าส่งมาก็ไม่ถูกใช้) เปลี่ยน `PENDING → APPROVED` และบันทึก `reviewer_id`, `reviewed_at`
**ขั้นตอนนี้ยังไม่ตัดสต็อก** ต้องเรียก `/issue` ต่อเพื่อจ่ายของจริง

```bash
curl.exe -X POST http://localhost:8080/api/v1/stock-requests/b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f/approve
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "message": "Stock request approved successfully",
  "data": {
    "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
    "request_number": "REQ-20261028-9F3A1B2C",
    "requester_id": "00000000-0000-4000-8000-000000000001",
    "status": "APPROVED",
    "notes": "ขอใช้แผนกบัญชี",
    "reviewer_id": "00000000-0000-4000-8000-000000000002",
    "reviewed_at": "2026-10-28T09:45:00.222222Z",
    "created_at": "2026-10-28T09:40:00.111111Z",
    "updated_at": "2026-10-28T09:45:00.222222Z",
    "items": [
      {
        "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
        "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
        "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
        "requested_quantity": 5
      }
    ]
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `INVALID_ID` | `:id` ไม่ใช่ UUID |
| 404 | `NOT_FOUND` | ไม่พบใบขอเบิก |
| 409 | `INVALID_STATE` | ใบไม่อยู่ในสถานะ PENDING (อนุมัติซ้ำ/ปฏิเสธไปแล้ว/จ่ายแล้ว) |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด |

```json
{
  "success": false,
  "error": { "code": "INVALID_STATE", "message": "Stock request cannot be processed in its current status" }
}
```

#### 2.5.5 ยกเลิกใบขอเบิก — `POST /api/v1/stock-requests/:id/cancel`

ยกเลิกใบขอที่ยังไม่ถูกจ่ายออกไป: `PENDING` หรือ `APPROVED` → `CANCELLED`
**ไม่แตะสต็อกและไม่สร้าง movement** (ใช้เมื่อต้องการยกเลิกคำขอ ไม่ใช่การปรับปรุงสต็อก)

- ไม่ต้องส่ง body
- ยกเลิกไม่ได้ถ้าสถานะเป็น `ISSUED`, `REJECTED`, `CANCELLED` → `409 INVALID_STATE`

```bash
curl.exe -X POST http://localhost:8080/api/v1/stock-requests/b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f/cancel
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "message": "Stock request cancelled successfully",
  "data": {
    "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
    "request_number": "REQ-20261028-9F3A1B2C",
    "requester_id": "00000000-0000-4000-8000-000000000001",
    "status": "CANCELLED",
    "notes": "ขอใช้แผนกบัญชี",
    "created_at": "2026-10-28T09:40:00.111111Z",
    "updated_at": "2026-10-28T09:55:00.555555Z",
    "items": [
      { "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
        "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
        "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
        "requested_quantity": 5 }
    ]
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `INVALID_ID` | `:id` ไม่ใช่ UUID |
| 404 | `NOT_FOUND` | ไม่พบใบขอเบิก |
| 409 | `INVALID_STATE` | ใบถูกจ่าย/ปฏิเสธ/ยกเลิกไปแล้ว (มี `details.current_status` และ `details.issuance_id` ถ้าจ่ายแล้ว) |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด |

```json
{
  "success": false,
  "error": {
    "code": "INVALID_STATE",
    "message": "Stock request cannot be processed in its current status",
    "details": { "current_status": "ISSUED", "issuance_id": "…", "issued_at": "2026-10-28T09:50:00.333333Z" }
  }
}
```

> ใบที่ยกเลิกแล้วยังค้นย้อนหลังได้ด้วย `GET /stock-requests?status=CANCELLED`

#### 2.5.6 จ่ายของจริง — `POST /api/v1/stock-requests/:id/issue`

ไม่ต้องส่ง body เปลี่ยน `APPROVED → ISSUED` และทำใน **transaction เดียว**:

**Headers (ไม่บังคับ)**

| Header | กฎ |
|--------|-----|
| `Idempotency-Key` | ยาว 8–100 ตัว อักขระ `A-Z a-z 0-9 . _ : -` — ใช้เมื่อต้องการให้ "ยิงซ้ำแล้วได้ผลเดิม" (เช่น client timeout แล้ว retry) ถ้าไม่ส่ง = at-most-once (ยิงซ้ำจะได้ `409`) |

ขั้นตอนใน transaction:

1. ล็อกแถวใบขอ (`FOR UPDATE`) และตรวจสถานะ
   - ถ้าเป็น `ISSUED` อยู่แล้ว **และส่ง key เดิมมา** → ตอบ `200` แบบ replay (ไม่ตัดสต็อกซ้ำ)
   - ถ้าเป็น `ISSUED` แต่ key ไม่ตรง/ไม่ส่ง → `409 INVALID_STATE`
2. ถ้าส่ง key มา → ตรวจว่า key นี้ไม่เคยผูกกับใบอื่น (ถ้าเคย → `409 IDEMPOTENCY_KEY_REUSED`)
3. ล็อกแถวสินค้าทุกตัวที่เกี่ยวข้อง (เรียงตาม id เพื่อลด deadlock)
4. ตรวจทุก item ก่อนแก้ยอด: มีอยู่ / `is_active` / สต็อกพอ (ถ้าขาด เก็บรายการที่ขาดไว้ทั้งหมด)
5. ตัดสต็อกทุกตัว (ห้ามติดลบ) + สร้าง movement `ISSUE` 1 แถวต่อ item (`reference_type = "STOCK_REQUEST"`, `reference_id = <request_number>`)
6. บันทึกแถว `stock_request_issuances` (audit + idempotency key, **UNIQUE ต่อ 1 ใบ** → จ่ายซ้ำไม่ได้แม้โค้ดพลาด)
7. เปลี่ยนสถานะเป็น `ISSUED` แล้ว commit

> ถ้ามี item ใดจ่ายไม่ได้ (สต็อกไม่พอ / สินค้าถูกปิด) → **ไม่จ่ายสักรายการ** ยอดสต็อกไม่ขยับ สถานะคงเป็น `APPROVED`
> ถ้าสต็อกไม่พอให้รับเข้าเพิ่ม (`POST /stock-receipts`) แล้วเรียก `/issue` ซ้ำได้

```bash
curl.exe -X POST http://localhost:8080/api/v1/stock-requests/b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f/issue
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "message": "Stock request issued successfully",
  "data": {
    "request": {
      "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
      "request_number": "REQ-20261028-9F3A1B2C",
      "requester_id": "00000000-0000-4000-8000-000000000001",
      "status": "ISSUED",
      "reviewer_id": "00000000-0000-4000-8000-000000000002",
      "reviewed_at": "2026-10-28T09:45:00.222222Z",
      "created_at": "2026-10-28T09:40:00.111111Z",
      "updated_at": "2026-10-28T09:50:00.333333Z",
      "items": [
        {
          "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
          "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
          "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
          "requested_quantity": 5
        }
      ]
    },
    "movements": [
      {
        "id": 21,
        "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
        "movement_type": "ISSUE",
        "quantity": 5,
        "reference_type": "STOCK_REQUEST",
        "reference_id": "REQ-20261028-9F3A1B2C",
        "actor_id": "00000000-0000-4000-8000-000000000002",
        "created_at": "2026-10-28T09:50:00.333333Z"
      }
    ],
    "issuance": {
      "id": "5e6f7a8b-9c0d-4e1f-8a2b-3c4d5e6f7a8b",
      "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
      "idempotency_key": "checkout-abc123",
      "actor_id": "00000000-0000-4000-8000-000000000002",
      "item_count": 1,
      "total_quantity": 5,
      "created_at": "2026-10-28T09:50:00.333333Z"
    }
  },
  "meta": {
    "issuance_id": "5e6f7a8b-9c0d-4e1f-8a2b-3c4d5e6f7a8b",
    "issued_at": "2026-10-28T09:50:00.333333Z",
    "idempotent_replay": false
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `INVALID_ID` | `:id` ไม่ใช่ UUID |
| 400 | `INVALID_IDEMPOTENCY_KEY` | `Idempotency-Key` สั้น/ยาวเกิน หรือมีอักขระต้องห้าม |
| 404 | `NOT_FOUND` | ไม่พบใบขอเบิก หรือไม่พบสินค้าที่อ้างถึง |
| 409 | `INVALID_STATE` | ใบยังไม่ APPROVED หรือถูก ISSUED ไปแล้ว (ดู `details.current_status`) |
| 409 | `INSUFFICIENT_STOCK` | สต็อกตัวใดตัวหนึ่งไม่พอ (ไม่จ่ายเลยสักรายการ) + `details.items` |
| 409 | `PRODUCT_INACTIVE` | สินค้าถูกปิดการใช้งานหลังอนุมัติ |
| 409 | `IDEMPOTENCY_KEY_REUSED` | key นี้ถูกใช้กับใบขออื่นไปแล้ว |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด (rollback ทั้งหมด) |

**กรณีสต็อกไม่พอ — บอกด้วยว่าขาดตัวไหน เท่าไร (ไม่มีการตัดสต็อกเลย)**

```json
{
  "success": false,
  "error": {
    "code": "INSUFFICIENT_STOCK",
    "message": "Insufficient stock to fulfil the requested items",
    "details": {
      "items": [
        { "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60", "requested": 10, "available": 3, "shortfall": 7 }
      ]
    }
  }
}
```

**กรณียิงซ้ำด้วย `Idempotency-Key` เดิม (200 — ได้ผลลัพธ์เดิม ไม่ตัดสต็อกซ้ำ)**

```json
{
  "success": true,
  "message": "Stock request already issued (idempotent replay)",
  "data": { "request": { "status": "ISSUED" }, "movements": [ { "id": 21, "quantity": 5 } ] },
  "meta": { "issuance_id": "…", "issued_at": "2026-10-28T09:50:00.333333Z", "idempotent_replay": true }
}
```

```json
{
  "success": false,
  "error": { "code": "INSUFFICIENT_STOCK", "message": "Insufficient stock to fulfil the requested items" }
}
```

#### 2.5.7 ปฏิเสธใบขอเบิก — `POST /api/v1/stock-requests/:id/reject`

เปลี่ยน `PENDING → REJECTED` บันทึก `reviewer_id`, `reviewed_at` และเหตุผล (ถ้ามี) **ไม่แตะสต็อกและไม่สร้าง movement**

**Body (ไม่บังคับ — จะไม่ส่ง body เลยก็ได้)**

| field | type | จำเป็น | กฎ |
|-------|------|---------|-----|
| `rejection_reason` | string | ❌ | ≤ 1000 ตัวอักษร |

```bash
# มีเหตุผล
curl.exe -X POST http://localhost:8080/api/v1/stock-requests/b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f/reject ^
  -H "Content-Type: application/json" ^
  -d "{\"rejection_reason\":\"งบประมาณไม่พอ\"}"

# ไม่มีเหตุผล
curl.exe -X POST http://localhost:8080/api/v1/stock-requests/b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f/reject
```

**สำเร็จ (200)**

```json
{
  "success": true,
  "message": "Stock request rejected successfully",
  "data": {
    "id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
    "request_number": "REQ-20261028-9F3A1B2C",
    "requester_id": "00000000-0000-4000-8000-000000000001",
    "status": "REJECTED",
    "notes": "ขอใช้แผนกบัญชี",
    "reviewer_id": "00000000-0000-4000-8000-000000000002",
    "reviewed_at": "2026-10-28T09:47:00.444444Z",
    "rejection_reason": "งบประมาณไม่พอ",
    "created_at": "2026-10-28T09:40:00.111111Z",
    "updated_at": "2026-10-28T09:47:00.444444Z",
    "items": [
      {
        "id": "c2d3e4f5-6a7b-4c8d-9e0f-1a2b3c4d5e6f",
        "request_id": "b1d2e3f4-5a6b-4c7d-8e9f-0a1b2c3d4e5f",
        "product_id": "3f6c1a2e-8b41-4c9d-9f7a-1b2c3d4e5f60",
        "requested_quantity": 5
      }
    ]
  }
}
```

**ข้อผิดพลาด**

| Status | code | เมื่อ |
|--------|------|-------|
| 400 | `INVALID_ID` | `:id` ไม่ใช่ UUID |
| 400 | `BAD_REQUEST` | ส่ง body มาแต่ไม่ใช่ JSON |
| 400 | `VALIDATION_ERROR` | `rejection_reason` ยาวเกิน 1000 |
| 404 | `NOT_FOUND` | ไม่พบใบขอเบิก |
| 409 | `INVALID_STATE` | ปฏิเสธซ้ำ หรือปฏิเสธใบที่อนุมัติ/จ่ายไปแล้ว |
| 500 | `INTERNAL_ERROR` | DB ผิดพลาด |

> ปฏิเสธซ้ำจะได้ `409 INVALID_STATE` เสมอ (ไม่ใช่ 200) เพื่อให้ client รู้ว่าสถานะไม่เปลี่ยน

### 2.6 ตัวอย่างการใช้งานครบวงจร (Copy ไปรันได้เลย)

ลำดับที่ต้องเรียก:

```
สร้างสินค้า → รับเข้า (stock-receipts) → สร้างใบขอ (stock-requests)
   → approve → issue → ตรวจ stock-movements
```

#### 2.6.1 PowerShell (แนะนำสำหรับ Windows)

```powershell
$base = "http://localhost:8080/api/v1"
$json = "application/json"

# ---------- 1) สร้างสินค้า 2 ตัว ----------
$p1 = Invoke-RestMethod -Method Post -Uri "$base/products" -ContentType $json -Body (@{
    sku = "SKU-A-001"; name = "Paper A4"; unit = "REAM"; min_stock = 5
} | ConvertTo-Json)
$p2 = Invoke-RestMethod -Method Post -Uri "$base/products" -ContentType $json -Body (@{
    sku = "SKU-B-001"; name = "Blue Pen"; unit = "BOX"; min_stock = 3
} | ConvertTo-Json)

$prod1 = $p1.data.id
$prod2 = $p2.data.id
"สินค้า 1 = $prod1"
"สินค้า 2 = $prod2"

# ---------- 2) รับของเข้าคลัง ----------
Invoke-RestMethod -Method Post -Uri "$base/stock-receipts" -ContentType $json -Body (@{
    product_id = $prod1; quantity = 40; notes = "รับจาก Supplier A"
} | ConvertTo-Json) | Out-Null

Invoke-RestMethod -Method Post -Uri "$base/stock-receipts" -ContentType $json -Body (@{
    product_id = $prod2; quantity = 20; notes = "รับจาก Supplier B"
} | ConvertTo-Json) | Out-Null

# ---------- 3) สร้างใบขอเบิก (PENDING) ----------
$reqBody = @{
    notes = "ขอใช้แผนกบัญชี"
    items = @(
        @{ product_id = $prod1; quantity = 10 },
        @{ product_id = $prod2; quantity = 4 }
    )
} | ConvertTo-Json -Depth 5

$req = Invoke-RestMethod -Method Post -Uri "$base/stock-requests" -ContentType $json -Body $reqBody
$reqID = $req.data.id
"ใบขอเบิก = $($req.data.request_number) สถานะ = $($req.data.status)"   # PENDING

# ---------- 4) อนุมัติ (ยังไม่ตัดสต็อก) ----------
$approved = Invoke-RestMethod -Method Post -Uri "$base/stock-requests/$reqID/approve"
"สถานะหลังอนุมัติ = $($approved.data.status)"                          # APPROVED

# ---------- 5) จ่ายของจริง (ตัดสต็อก + สร้าง ISSUE movement) ----------
$issued = Invoke-RestMethod -Method Post -Uri "$base/stock-requests/$reqID/issue"
"สถานะหลังจ่าย = $($issued.data.request.status)"                       # ISSUED
$issued.data.movements | Format-Table id, movement_type, quantity, reference_id

# ---------- 6) ตรวจสต็อกและประวัติ ----------
Invoke-RestMethod -Uri "$base/products/$prod1" | Select-Object -ExpandProperty data |
    Format-Table sku, current_stock, min_stock          # current_stock = 30 (40-10)

Invoke-RestMethod -Uri "$base/stock-movements?product_id=$prod1&limit=5" |
    Select-Object -ExpandProperty data | Format-Table id, movement_type, quantity, reference_type
```

#### 2.6.2 bash / Git Bash (ใช้ `jq` ช่วยอ่านค่า)

> ถ้าเครื่องไม่มี `jq` ให้คัดลอก `data.id` จาก response เองแล้วใส่เป็นตัวแปร

```bash
BASE=http://localhost:8080/api/v1
JSON="Content-Type: application/json"

# 1) สร้างสินค้า
PROD1=$(curl -s -X POST $BASE/products -H "$JSON" \
  -d '{"sku":"SKU-A-001","name":"Paper A4","unit":"REAM","min_stock":5}' | jq -r '.data.id')
PROD2=$(curl -s -X POST $BASE/products -H "$JSON" \
  -d '{"sku":"SKU-B-001","name":"Blue Pen","unit":"BOX","min_stock":3}' | jq -r '.data.id')
echo "PROD1=$PROD1  PROD2=$PROD2"

# 2) รับเข้า 40 และ 20
curl -s -X POST $BASE/stock-receipts -H "$JSON" \
  -d "{\"product_id\":\"$PROD1\",\"quantity\":40}" > /dev/null
curl -s -X POST $BASE/stock-receipts -H "$JSON" \
  -d "{\"product_id\":\"$PROD2\",\"quantity\":20}" > /dev/null

# 3) สร้างใบขอเบิก
REQ=$(curl -s -X POST $BASE/stock-requests -H "$JSON" -d "{
  \"notes\":\"monthly consumables\",
  \"items\":[
    {\"product_id\":\"$PROD1\",\"quantity\":10},
    {\"product_id\":\"$PROD2\",\"quantity\":4}
  ]}")
REQ_ID=$(echo "$REQ" | jq -r '.data.id')
echo "$REQ" | jq '{status:.data.status, number:.data.request_number}'   # PENDING

# 4) อนุมัติ แล้ว 5) จ่ายของ
curl -s -X POST $BASE/stock-requests/$REQ_ID/approve | jq '.data.status'   # APPROVED
curl -s -X POST $BASE/stock-requests/$REQ_ID/issue | jq '{status:.data.request.status, movements:[.data.movements[]|{product_id,quantity}]}'

# 6) ตรวจสต็อก + ประวัติ
curl -s $BASE/products/$PROD1 | jq '.data | {sku, current_stock}'         # 30
curl -s "$BASE/stock-movements?product_id=$PROD1&limit=5" | jq '.data[] | {movement_type,quantity,reference_type}'
```

#### 2.6.3 ตัวอย่างกรณีผิดพลาดที่ควรรู้

| สถานการณ์ | ผลลัพธ์ |
|-----------|---------|
| อนุมัติใบเดิมซ้ำ | `409 INVALID_STATE` (ไม่ตัดสต็อกซ้ำ) |
| จ่ายของตอนที่ใบยัง PENDING | `409 INVALID_STATE` |
| จ่ายของแต่สต็อกไม่พอ | `409 INSUFFICIENT_STOCK` + ไม่มีอะไรเปลี่ยนเลย |
| รับเข้าสินค้าที่ปิดใช้งาน | `400 PRODUCT_INACTIVE` |
| ขอเบิกสินค้าที่ไม่มีในระบบ | `404 NOT_FOUND` |
| ใส่สินค้าซ้ำในใบเดียว | `400 DUPLICATE_ITEM` |
| ยิง `/issue` พร้อมกัน 8 ครั้ง | สำเร็จ 1 ครั้ง, อีก 7 ครั้งได้ `409 INVALID_STATE`, สต็อกถูกตัดครั้งเดียว |

### 2.7 ข้อจำกัดที่ต้องรู้ก่อนใช้งานจริง

| หัวข้อ | รายละเอียด |
|--------|------------|
| **ต้องล็อกอินก่อนใช้งาน** | ตั้งแต่ Phase 7 ทุก endpoint ธุรกิจต้องมี `Authorization: Bearer <token>` (ยกเว้น `/health`, `/ready`, `POST /auth/login`) — ดู §3 |
| **บังคับสิทธิ์ตาม role + เจ้าของข้อมูล** | REQUESTER เห็น/ยกเลิกเฉพาะใบของตัวเอง; approve/reject/issue/receipts = STOREKEEPER หรือ ADMIN; users/audit = ADMIN |
| **ผู้กระทำมาจาก session token เท่านั้น** | dev identities ของ Phase 5 ถูกตั้ง `is_active = false` (ล็อกอินไม่ได้) และระบบไม่อ่าน actor จาก body |
| **client กำหนดผู้กระทำไม่ได้** | ถ้าส่ง `requester_id`/`reviewer_id`/`actor_id` มาใน body ระบบจะ ignore ทั้งหมด |
| **มี endpoint จัดการ user (ADMIN)** | `/api/v1/users` (สร้าง/ดู/แก้ role/ปิดใช้งาน/reset password) + `cmd/user` สำหรับ bootstrap |
| **ยกเลิกได้เฉพาะก่อนจ่าย** | ยกเลิกได้ที่สถานะ `PENDING`/`APPROVED` → `CANCELLED`; ใบที่ `ISSUED`/`REJECTED`/`CANCELLED` ยกเลิกไม่ได้ (409) และยังไม่มีการเก็บเหตุผลการยกเลิก |
| **ไม่รองรับ partial fulfillment** | ใบขอเป็นแบบ all-or-nothing: ถ้าสต็อกไม่พอจะไม่จ่ายเลยสักรายการ และแจ้งรายการที่ขาดใน `details.items` (ต้องรับเข้าเพิ่มแล้วยิงซ้ำ หรือยกเลิกแล้วสร้างใบใหม่) |
| **`Idempotency-Key` ไม่บังคับ** | ถ้าไม่ส่ง key แล้วยิง `/issue` ซ้ำ จะได้ 409 (ไม่ replay) — client ควรถือว่า `409` + `current_status = ISSUED` คือ "จ่ายไปแล้ว" |
| **ledger แก้/ลบไม่ได้** | ไม่มี endpoint update/delete ของ `stock_movements` (ตามดีไซน์) ถ้าบันทึกผิดต้องรับเข้า/ปรับปรุงด้วยรายการใหม่ |
| **สต็อกเป็นจำนวนเต็ม** | `current_stock` เป็น `INT` (สูงสุด 2,147,483,647) ไม่มีการแปลงหน่วย (unit) อัตโนมัติ |
| **`PUT /products/:id` ไม่ใช่ partial update** | ถ้าไม่ส่ง `min_stock` จะถูกตั้งเป็น `0` และ `description` จะถูกแทนที่ด้วยค่าว่าง |
| **list สินค้าซ่อนสินค้าปิดใช้งาน** | ต้องส่ง `is_active=false` หรือ `is_active=all` จึงจะเห็น |
| **`meta.limit` ของ `GET /products`** | ค่าที่ส่งมาจะถูกสะท้อนใน `meta` แต่ระบบดึงจริงไม่เกิน 100 รายการ → แนะนำส่ง `limit` ≤ 100 |
| **pagination เป็นแบบ offset** | ข้อมูลเปลี่ยนระหว่างเปิดหน้าอาจทำให้ลำดับเลื่อน (ถ้าต้องการความแม่นยำสูงต้องใช้ cursor ซึ่งยังไม่มี) |
| **performance** | index มีที่ `product_id`, `created_at`, `reference_type/reference_id` แต่ไม่มี index เฉพาะ `movement_type` → ถ้ากรองด้วย `movement_type` อย่างเดียวบนข้อมูลมากอาจช้า (เพิ่ม migration index ภายหลังได้) |
| **integration test ต้องมี DB** | ถ้าไม่มี PostgreSQL ในเครื่อง เทสต์จะ skip (ไม่ถือว่า fail) — ดูข้อ 1.6 |
| **ไม่มี rate limit / CORS** | ถ้าจะเรียกจากเบราว์เซอร์คนละ origin ต้องเพิ่ม middleware เอง |
| **เวลาเป็น UTC** | ทุก timestamp เป็น UTC (แสดง `Z`) ถ้าต้องการเวลาไทยให้แปลงฝั่ง client (+7 ชั่วโมง) |
| **Docker ไม่รองรับ** | โปรเจกต์นี้ออกแบบให้รันกับ PostgreSQL local เท่านั้น |

---

## 3. Authentication & Authorization (Phase 7)

ตั้งแต่ Phase 7 เป็นต้นไป **ทุก endpoint ของธุรกิจต้องมี Bearer token** (ยกเว้น `/health`, `/ready`, `POST /auth/login`)

### 3.1 ขั้นตอนแรก: สร้างผู้ดูแลระบบ (bootstrap admin)

ไม่มีรหัสผ่านใด ๆ ถูกเก็บใน repo — ใช้ CLI + environment variable

```bash
# Windows PowerShell
$env:BOOTSTRAP_ADMIN_USERNAME="admin"
$env:BOOTSTRAP_ADMIN_PASSWORD="<รหัสผ่านที่คาดเดายาก ≥ 8 ตัว>"
go run ./cmd/user -action create

# เปลี่ยนรหัสผ่านภายหลัง
go run ./cmd/user -action reset-password
```

- CLI จะพิมพ์เฉพาะ `username` และ `id` ไม่พิมพ์รหัสผ่าน
- รหัสผ่านถูกเก็บเป็น **PBKDF2-HMAC-SHA256** (salted, 600,000 รอบ โดยค่าเริ่มต้น) ใน `users.password_hash`
- dev identities จาก Phase 5 (`dev_requester`, `dev_storekeeper`) ถูกตั้ง `is_active = false` → ล็อกอินไม่ได้ (ยังคงอยู่เพื่ออ้างอิงประวัติ)

### 3.2 Login / Logout / Me / Change password

| Method | Path | คำอธิบาย |
|---|---|---|
| POST | `/api/v1/auth/login` | public — คืน token (Bearer) + `expires_at` + ข้อมูลผู้ใช้ |
| POST | `/api/v1/auth/logout` | revoke session ปัจจุบัน (ต้องมี token) |
| GET | `/api/v1/auth/me` | ข้อมูลผู้ใช้ปัจจุบัน |
| POST | `/api/v1/auth/change-password` | เปลี่ยนรหัสผ่าน; **revoke ทุก session** หลังเปลี่ยน |

```bash
curl.exe -X POST http://localhost:8080/api/v1/auth/login ^
  -H "Content-Type: application/json" ^
  -d "{\"username\":\"admin\",\"password\":\"<รหัสผ่าน>\"}"
```

```json
{
  "success": true,
  "message": "Login successful",
  "data": {
    "token": "3Qh...43chars",
    "token_type": "Bearer",
    "expires_at": "2026-10-28T22:00:00Z",
    "user": { "id": "…", "username": "admin", "role": "ADMIN", "is_active": true }
  }
}
```

ใช้ token กับทุก request:
```bash
curl.exe http://localhost:8080/api/v1/stock-requests -H "Authorization: Bearer <token>"
```

- token เป็นแบบ **opaque** (ไม่ใช่ JWT) — เก็บใน DB เป็น HMAC-SHA256 hash; รีสตาร์ทหรือ logout แล้วใช้ไม่ได้ทันที
- อายุ token ตั้งได้ด้วย `AUTH_TOKEN_TTL` (default 12h)
- ล็อกอินผิดติดกันเกิน `AUTH_MAX_LOGIN_ATTEMPTS` (default 8 ครั้ง / 15 นาที) → `429 TOO_MANY_ATTEMPTS`

### 3.3 Role matrix

| Endpoint | REQUESTER | STOREKEEPER | ADMIN |
|---|---|---|---|
| `GET /health`, `GET /ready`, `POST /auth/login` | ✅ public | ✅ public | ✅ public |
| `/auth/logout`, `/auth/me`, `/auth/change-password` | ✅ | ✅ | ✅ |
| `GET /products`, `GET /products/:id` | ✅ | ✅ | ✅ |
| `POST /products`, `PUT /products/:id`, `PATCH /products/:id/deactivate` | ❌ 403 | ✅ | ✅ |
| `GET /stock-movements` | ✅ | ✅ | ✅ |
| `POST /stock-receipts` | ❌ 403 | ✅ | ✅ |
| `POST /stock-requests` | ✅ | ✅ | ✅ |
| `GET /stock-requests`, `GET /stock-requests/:id` | ✅ **เฉพาะของตัวเอง** | ✅ ทั้งหมด | ✅ ทั้งหมด |
| `POST /stock-requests/:id/approve`, `/reject`, `/issue` | ❌ 403 | ✅ | ✅ |
| `POST /stock-requests/:id/cancel` | ✅ เฉพาะของตัวเอง | ✅ ทุกใบ | ✅ ทุกใบ |
| `/users/*` (สร้าง/ดู/แก้ role/ปิดใช้งาน/รีเซ็ตรหัส) | ❌ 403 | ❌ 403 | ✅ |
| `GET /audit-events` | ❌ 403 | ❌ 403 | ✅ |

**กฎ four-eyes:** ผู้ขอ **ห้ามอนุมัติ / ปฏิเสธ / จ่ายของใบของตัวเอง** → `403 SELF_APPROVAL_FORBIDDEN`
**กันล็อกตัวเองออก:** ADMIN ปิดใช้งานบัญชีตัวเองไม่ได้ (`403 FORBIDDEN`) และ ADMIN คนสุดท้ายถูกปลด/ปิดไม่ได้ (`409 LAST_ADMIN`)

### 3.4 ยกเลิกใบขอ (มีเหตุผลได้)

```bash
curl.exe -X POST http://localhost:8080/api/v1/stock-requests/<id>/cancel ^
  -H "Authorization: Bearer <token>" ^
  -H "Content-Type: application/json" ^
  -d "{\"reason\":\"แผนกเปลี่ยนความต้องการ\"}"
```
- `reason` ไม่บังคับ (≤ 1000 ตัวอักษร) — เก็บใน `stock_requests.cancellation_reason` และ `audit_events.reason`
- ไม่แตะสต็อก ไม่สร้าง movement

### 3.5 Audit trail (ADMIN)

```bash
curl.exe "http://localhost:8080/api/v1/audit-events?event_type=REQUEST_ISSUED&limit=20&offset=0" ^
  -H "Authorization: Bearer <token>"
```

Query ที่รองรับ: `event_type`, `entity_type`, `entity_id`, `actor_id`, `from`, `to` (RFC3339), `limit` (≤200), `offset`
Response มี `meta {limit, offset, total}` และแต่ละ event มี `event_type`, `actor_username`, `actor_role`, `entity_type`, `entity_id`, `reference`, `from_status`, `to_status`, `reason`, `metadata`, `created_at`

**Event types หลัก:** `AUTH_LOGIN_SUCCEEDED`, `AUTH_LOGIN_FAILED`, `AUTH_LOGOUT`, `AUTH_PASSWORD_CHANGED`, `ACCESS_DENIED`, `SELF_APPROVAL_BLOCKED`, `PRODUCT_CREATED/UPDATED/DEACTIVATED`, `STOCK_RECEIVED`, `REQUEST_CREATED/APPROVED/REJECTED/CANCELLED/ISSUED`, `USER_CREATED/ROLE_CHANGED/ACTIVATED/DEACTIVATED/PASSWORD_RESET`

- การเปลี่ยนสถานะธุรกิจ (สร้าง/อนุมัติ/ปฏิเสธ/ยกเลิก/จ่ายของ/รับเข้า) เขียน audit **ใน transaction เดียวกัน** → rollback = ไม่มี audit ปลอม
- เหตุการณ์ความปลอดภัย (login ผิด, 403) เขียนแบบ best-effort (ไม่ทำให้ request ล้มเหลว)

### 3.6 Error codes ที่เพิ่มใน Phase 7

| Status | code | เมื่อ |
|---|---|---|
| 401 | `UNAUTHORIZED` | ไม่มี/ผิดรูปแบบ/หมดอายุ/ถูก revoke token, บัญชีถูกปิด |
| 401 | `INVALID_CREDENTIALS` | username/password ไม่ถูก (ไม่บอกว่าใครผิด) |
| 403 | `FORBIDDEN` | role ไม่พอ / ปิดใช้งานบัญชีตัวเอง |
| 403 | `SELF_APPROVAL_FORBIDDEN` | ผู้ขออนุมัติ/จ่าย/ปฏิเสธใบของตัวเอง |
| 409 | `IDEMPOTENCY_KEY_REUSED` | (Phase 6) key ซ้ำคนละใบ |
| 409 | `DUPLICATE_USERNAME` / `LAST_ADMIN` | username ซ้ำ / ปลด ADMIN คนสุดท้าย |
| 400 | `WEAK_PASSWORD` | รหัสผ่านสั้นกว่า 8 ตัว หรือเหมือน username |
| 429 | `TOO_MANY_ATTEMPTS` | ล็อกอินผิดติดกันเกินกำหนด |

### 3.7 ข้อจำกัดที่ยังเหลือ

- ยังไม่มี MFA, OAuth/OIDC, รีเซ็ตรหัสผ่านทางอีเมล, หน้าจอ login
- rate limit ของ login เก็บใน memory (ต่อ process) — multi-instance ต้องย้ายไป shared store
- token ส่งผ่าน HTTP ธรรมดาในเครื่อง dev — production ต้องมี TLS ที่ reverse proxy
- audit เก็บ IP/user-agent (PII) — ต้องมีนโยบายลบ/เก็บตามข้อกำหนดองค์กร
- `AUTH_TOKEN_PEPPER` ต้องเปลี่ยนจากค่า dev ก่อนใช้งานจริง และห้าม commit ค่าจริง

---

## ภาคผนวก: คำสั่งและไฟล์ที่ควรรู้

### คำสั่งที่ใช้บ่อย

```bash
# ดูสถานะ migration
go run ./cmd/migrate -action status

# apply migration ที่ค้างอยู่
go run ./cmd/migrate -action up

# รัน API
go run ./cmd/api

# ตรวจคุณภาพโค้ด + รันเทสต์ทั้งหมด
go vet ./...
go test ./...

# รันเฉพาะเทสต์ของ stock request
go test ./internal/usecase/stockrequest ./internal/adapter/http/fiber/... -run StockRequest -v

# รัน integration test (ต้องมี DB)
set TEST_DATABASE_URL=postgres://stockflow_user:stockflow_dev_password@127.0.0.1:5432/stockflow_db?sslmode=disable
go test ./... -count=1
```

### แผนที่ไฟล์สำคัญ

| ไฟล์ / โฟลเดอร์ | หน้าที่ |
|-----------------|---------|
| `cmd/api/main.go` | จุดเริ่มโปรแกรม, ประกอบ repository/usecase/handler, graceful shutdown |
| `cmd/migrate/main.go` | CLI migration (`up`/`down`/`status`) |
| `internal/config/config.go` | อ่าน env (+ ไฟล์ `.env`) + validate + สร้าง connection string |
| `internal/config/dotenv.go` | ตัวโหลด `.env` แบบไม่มี dependency ภายนอก (OS env ชนะ `.env` เสมอ) |
| `internal/infrastructure/database/` | pgxpool, migrator, readiness check |
| `internal/domain/` | entity + กฎธุรกิจ + error + interface repository |
| `internal/domain/stock_issuance.go` | โครงสร้าง issuance / stock shortage + typed error ที่พา `details` ไปถึง client |
| `internal/usecase/product/` | logic สินค้า |
| `internal/usecase/stockmovement/` | logic รับเข้า + ประวัติ movement |
| `internal/usecase/stockrequest/` | logic ใบขอเบิก/อนุมัติ/จ่ายของ + development identity |
| `internal/adapter/http/fiber/router.go` | นิยาม route ทั้งหมด |
| `internal/adapter/http/fiber/handler/` | HTTP handler + การ map error เป็น status code |
| `internal/adapter/repository/postgres/` | SQL ทั้งหมด (parameterized) + transaction/lock |
| `migrations/*.sql` | schema + seed (versioned) |
| `pkg/response/response.go` | response envelope กลาง |
| `scripts/setup_local_db.sql` | สคริปต์สร้าง role/database สำหรับเครื่อง local (ดูข้อ 1.2.1) |

### ตรวจสอบข้อมูลตรง ๆ ในฐานข้อมูล (อ่านเท่านั้น)

```sql
-- ดูสินค้าและสต็อกปัจจุบัน
SELECT sku, name, current_stock, min_stock, is_active FROM products ORDER BY created_at DESC;

-- ดูใบขอเบิกล่าสุดพร้อมรายการ
SELECT r.request_number, r.status, r.reviewed_at, i.product_id, i.requested_quantity
FROM stock_requests r
JOIN stock_request_items i ON i.request_id = r.id
ORDER BY r.created_at DESC
LIMIT 20;

-- ดู movement ล่าสุด (บวก = เข้า, ลบ = ออกตาม movement_type)
SELECT id, product_id, movement_type, quantity, reference_type, reference_id, created_at
FROM stock_movements
ORDER BY created_at DESC
LIMIT 20;
```

> คำสั่ง SQL ข้างต้นเป็นแบบอ่านข้อมูลเท่านั้น ไม่ควร `UPDATE`/`DELETE` ข้อมูลจริงด้วยมือ เพราะจะทำให้ยอดสต็อกกับ ledger ไม่ตรงกัน
