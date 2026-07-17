#!/bin/bash
set -e

# This script initializes both goget and dex databases
# It runs automatically when the PostgreSQL container starts for the first time

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
    -- Ensure goget database exists (should already exist from POSTGRES_DB env var)
    SELECT 'Database goget already exists' WHERE EXISTS (SELECT FROM pg_database WHERE datname = 'goget');
    
    -- Create dex database for authentication
    CREATE DATABASE dex;
    GRANT ALL PRIVILEGES ON DATABASE dex TO goget;
    
    -- Connect to dex database and grant schema privileges
    \c dex;
    GRANT ALL ON SCHEMA public TO goget;
    
    -- Switch back to goget database
    \c goget;
    
    -- Ensure goget user has all privileges on goget database
    GRANT ALL PRIVILEGES ON DATABASE goget TO goget;
EOSQL

echo "Databases initialized successfully!"