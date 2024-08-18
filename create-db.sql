SELECT 'CREATE DATABASE ragtag'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'ragtag')\gexec