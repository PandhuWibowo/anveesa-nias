import type { DbDriver } from '@/composables/useConnections'

export const DRIVER_COLOR: Record<string, string> = {
  sqlite: '#4b5563', postgres: '#336791', mysql: '#c47a00', mariadb: '#a5352a', mssql: '#b8262a',
  redis: '#b52a25', memcache: '#15803d', kafka: '#2b2b2e', mongodb: '#00804a', cassandra: '#1f62c9',
  elasticsearch: '#00857c', opensearch: '#005eb8', s3_aws: '#b45309', s3_gcp: '#3367d6', s3_oss: '#c2410c', s3_obs: '#a00000',
  alibaba_sls: '#c2410c',
}

export const DRIVER_NAME: Record<string, string> = {
  sqlite: 'SQLite', postgres: 'PostgreSQL', mysql: 'MySQL', mariadb: 'MariaDB', mssql: 'SQL Server',
  redis: 'Redis', memcache: 'Memcache', kafka: 'Kafka', mongodb: 'MongoDB', cassandra: 'Cassandra',
  elasticsearch: 'Elasticsearch', opensearch: 'OpenSearch', s3_aws: 'AWS S3', s3_gcp: 'Google Cloud Storage',
  s3_oss: 'Alibaba OSS', s3_obs: 'Huawei OBS', alibaba_sls: 'Alibaba Cloud SLS',
}

/** The page a connection opens into when picked from the switcher or palette. */
export function connectionRoute(driver: DbDriver | string): string {
  switch (driver) {
    case 'redis': return 'redis'
    case 'memcache': return 'memcache'
    case 'kafka': return 'kafka'
    case 'mongodb': return 'mongodb'
    case 'cassandra': return 'cassandra'
    case 'elasticsearch':
    case 'opensearch': return 'search'
    case 's3_aws':
    case 's3_gcp':
    case 's3_oss':
    case 's3_obs': return 'cloud-storage'
    case 'alibaba_sls': return 'sls'
    default: return 'data'
  }
}
