import { format } from 'sql-formatter'

const dialectMap: Record<string, string> = {
  postgres: 'postgresql',
  mysql: 'mysql',
  sqlserver: 'tsql',
}

export function formatSQL(sql: string, driver = 'sql'): string {
  try {
    return format(sql, {
      language: (dialectMap[driver] ?? 'sql') as any,
      tabWidth: 2,
      keywordCase: 'upper',
      linesBetweenQueries: 1,
    })
  } catch {
    return sql
  }
}
