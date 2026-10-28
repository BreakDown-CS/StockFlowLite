# Backup & Restore — StockFlow Lite (PostgreSQL 18, Windows)

เอกสารนี้ใช้กับเครื่อง local/on-premise ที่ติดตั้ง PostgreSQL เอง (ไม่มี Docker)
ห้ามใส่รหัสผ่านในไฟล์นี้หรือใน git — ใช้ environment variable `PGPASSWORD` เฉพาะหน้าต่างที่รัน

Path ของเครื่องมือบนเครื่องนี้: `C:\Program Files\PostgreSQL\18\bin\`

---

## 1. สิ่งที่ต้องสำรอง

| รายการ | เหตุผล |
|---|---|
| ฐานข้อมูล `stockflow_db` ทั้งก้อน | มีทั้งตารางธุรกิจ, `stock_movements` (ledger ห้ามแก้), `audit_events`, `auth_sessions`, `schema_migrations` |
| ไฟล์ `.env` (นอกเหนือจาก repo) | เก็บ `DB_*` และ `AUTH_TOKEN_PEPPER` — เก็บใน password manager ไม่ใช่ git |
| ไฟล์ migration (`migrations/`) | มาจาก git อยู่แล้ว แต่ควร备份คู่กับ dump ของรอบนั้น |

> ไม่ต้อง backup อะไรเพิ่มเป็นพิเศษ: ยอดสต็อกคำนวณจากตารางในฐานข้อมูลเดียวกัน

---

## 2. สำรองข้อมูล (pg_dump)

```bat
set PGPASSWORD=<รหัสผ่านของ stockflow_user>

"C:\Program Files\PostgreSQL\18\bin\pg_dump.exe" ^
  -h 127.0.0.1 -p 5432 -U stockflow_user -d stockflow_db ^
  -Fc -f "D:\backups\stockflow_20261028_1430.dump"

set PGPASSWORD=
```

- `-Fc` = custom format (บีบอัด, restore เลือกตารางได้, ใช้กับ `pg_restore` ได้)
- ตั้งชื่อไฟล์ให้มีวันที่/เวลา และเก็บไว้นอกโฟลเดอร์ repo
- ตรวจว่าไฟล์ไม่เป็นศูนย์: `dir D:\backups\stockflow_*.dump`

**ตารางแนะนำ (ปรับตามนโยบายองค์กร)**

| ประเภท | ความถี่ | เก็บย้อนหลัง |
|---|---|---|
| Full dump | รายวัน (เช่น 02:00) | 14 วัน |
| Full dump | รายสัปดาห์ | 8 สัปดาห์ |
| ก่อนรัน migration ทุกครั้ง | ทุกครั้ง | 1 ชุดต่อ release |

---

## 3. ทดสอบกู้คืน (Restore drill) — ทำบนฐานข้อมูลแยกเสมอ

```bat
set PGPASSWORD=<รหัสผ่านของ postgres หรือ role ที่สร้าง DB ได้>

:: 3.1 สร้างฐานข้อมูลสำหรับทดสอบ (ห้ามใช้ชื่อ stockflow_db)
"C:\Program Files\PostgreSQL\18\bin\psql.exe" -h 127.0.0.1 -U postgres -d postgres ^
  -c "CREATE DATABASE stockflow_restore_check OWNER stockflow_user;"

:: 3.2 restore เข้า DB ใหม่
"C:\Program Files\PostgreSQL\18\bin\pg_restore.exe" ^
  -h 127.0.0.1 -p 5432 -U stockflow_user -d stockflow_restore_check ^
  --no-owner --no-privileges ^
  "D:\backups\stockflow_20261028_1430.dump"

set PGPASSWORD=
```

ตรวจผลลัพธ์:

```bat
:: ชี้ไปที่ DB ที่ restore แล้ว
set DB_NAME=stockflow_restore_check
set PGPASSWORD=<รหัสผ่านของ stockflow_user>

go run ./cmd/migrate -action status          :: ทุกเวอร์ชันควรเป็น YES
go run ./cmd/maintenance -action check-stock :: ควรได้ "no stock drift detected"
go run ./cmd/api                             :: smoke: login + GET /api/v1/products

set PGPASSWORD=
```

หลังตรวจเสร็จ ให้ลบฐานข้อมูลทดสอบ (ต้องเป็นฐานข้อมูลที่สร้างเพื่อการนี้เท่านั้น):

```bat
"C:\Program Files\PostgreSQL\18\bin\psql.exe" -h 127.0.0.1 -U postgres -d postgres ^
  -c "DROP DATABASE stockflow_restore_check;"
```

---

## 4. กู้คืนจริง (กรณีฐานข้อมูลเสียหาย)

1. แจ้งผู้ใช้งานและหยุดแอป (`Ctrl+C` ที่ `go run ./cmd/api`)
2. สำรองฐานข้อมูลปัจจุบันไว้ก่อน (ถ้ายังเข้าถึงได้): `pg_dump ... -Fc -f stockflow_before_restore.dump`
3. สร้างฐานข้อมูลใหม่ชื่อเดิม (หรือชื่ออื่นแล้วแก้ `DB_NAME`)
4. `pg_restore` จาก dump ล่าสุด
5. รัน `go run ./cmd/migrate -action up` (idempotent, มี advisory lock)
6. รัน `go run ./cmd/maintenance -action check-stock` → ต้องได้ drift 0
7. เริ่มแอปและตรวจ `/ready`
8. บันทึกเหตุการณ์ใน audit/ops log และแจ้งผู้เกี่ยวข้อง

> ⚠️ **ห้ามใช้ `pg_restore --clean` กับฐานข้อมูล production** โดยไม่มีการอนุมัติและการสำรองข้อมูลก่อน เพราะ `--clean` จะลบ object เดิมทิ้ง
> ⚠️ ถ้า restore ทับฐานข้อมูลเดิมโดยไม่สร้างใหม่ ให้ตรวจว่าไม่มี connection ค้าง (แอป/psql) มิฉะนั้น restore จะล้มเหลว

---

## 5. หลังการกู้คืน: ตรวจความสอดคล้อง

`cmd/maintenance -action check-stock` จะเทียบ `products.current_stock` กับ ledger
ถ้าพบ drift (exit code 2) ให้รายงานและ **ห้ามแก้ยอดอัตโนมัติ** — ต้องให้เจ้าของข้อมูลอนุมัติวิธีแก้ (เช่น บันทึก adjustment ผ่านกระบวนการที่กำหนด)

---

## 6. รายการตรวจสอบรายเดือน

- [ ] ไฟล์ backup ล่าสุดสร้างสำเร็จและมีขนาดสมเหตุสมผล
- [ ] ทดสอบ restore drill อย่างน้อย 1 ครั้ง/เดือน
- [ ] `migrate -action status` = YES ทุกเวอร์ชัน
- [ ] `maintenance -action check-stock` = ไม่มี drift
- [ ] `maintenance -action purge-sessions -older-than 168h` ทำงาน (ตาราง `auth_sessions` ไม่โตผิดปกติ)
- [ ] ไม่มีรหัสผ่าน/pepper ถูก commit ใน git (`git log -p -- .env` ต้องไม่มี)
