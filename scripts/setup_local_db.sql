-- ============================================================================
-- StockFlow Lite — ตั้งค่าฐานข้อมูลสำหรับเครื่อง local (Windows)
-- ใช้กับ PostgreSQL 13+ (ทดสอบกับ PostgreSQL 18)
--
-- วิธีรัน (ต้องใช้ psql เพราะมีคำสั่ง \gexec):
--   "C:\Program Files\PostgreSQL\18\bin\psql.exe" -U postgres -h 127.0.0.1 -p 5432 -d postgres -f scripts\setup_local_db.sql
--   (จะถามรหัสผ่านของ user postgres ที่ตั้งไว้ตอนติดตั้ง)
--
-- สคริปต์นี้ "ไม่ลบ" ข้อมูลใด ๆ:
--   * ถ้ามี role stockflow_user แล้ว  -> รีเซ็ตรหัสผ่านให้ตรงกับ .env.example
--   * ถ้ายังไม่มี                     -> สร้างให้
--   * ถ้ามี database stockflow_db แล้ว -> ตั้งให้ stockflow_user เป็นเจ้าของ
--   * ถ้ายังไม่มี                     -> สร้างให้ (เจ้าของคือ stockflow_user)
-- ============================================================================

\set ON_ERROR_STOP on

-- 1) role -------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'stockflow_user') THEN
        ALTER ROLE stockflow_user WITH LOGIN PASSWORD 'stockflow_dev_password';
        RAISE NOTICE 'updated password for existing role: stockflow_user';
    ELSE
        CREATE ROLE stockflow_user WITH LOGIN PASSWORD 'stockflow_dev_password';
        RAISE NOTICE 'created role: stockflow_user';
    END IF;
END
$$;

-- 2) database ---------------------------------------------------------------
-- CREATE DATABASE รันใน DO block ไม่ได้ จึงใช้ \gexec ของ psql
SELECT 'CREATE DATABASE stockflow_db OWNER stockflow_user'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'stockflow_db')
\gexec

-- 3) ถ้ามี database อยู่แล้วแต่เจ้าของเป็น user อื่น ----------------------------
ALTER DATABASE stockflow_db OWNER TO stockflow_user;
GRANT ALL PRIVILEGES ON DATABASE stockflow_db TO stockflow_user;

-- 4) ตรวจผลลัพธ์ -------------------------------------------------------------
SELECT rolname, rolcanlogin AS can_login FROM pg_roles WHERE rolname = 'stockflow_user';
SELECT datname, pg_get_userbyid(datdba) AS owner FROM pg_database WHERE datname = 'stockflow_db';

-- 5) (ไม่บังคับ) ถ้าเคยรัน migration ด้วย user อื่นจนมีตารางอยู่แล้ว
--    ให้เอาคอมเมนต์ออกแล้วรัน เพื่อย้ายเจ้าของตารางมาที่ stockflow_user
-- \c stockflow_db
-- REASSIGN OWNED BY postgres TO stockflow_user;
-- GRANT ALL ON SCHEMA public TO stockflow_user;
