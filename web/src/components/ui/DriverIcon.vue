<script setup lang="ts">
import type { DbDriver } from '@/composables/useConnections'

defineProps<{
  driver: DbDriver
  size?: number
}>()
</script>

<template>
  <svg
    :width="size ?? 20"
    :height="size ?? 20"
    viewBox="0 0 24 24"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    aria-hidden="true"
  >
    <!-- ── PostgreSQL: Slonik elephant ────────────────────────── -->
    <g v-if="driver === 'postgres'">
      <!-- ear -->
      <ellipse cx="7.5" cy="7.5" rx="3" ry="4" fill="white" opacity="0.55"/>
      <!-- head -->
      <circle cx="13" cy="10" r="7.5" fill="white" opacity="0.92"/>
      <!-- trunk -->
      <path d="M5.8 10C4.8 11 4.2 12.8 5 14.5C5.5 15.5 6.5 16 7.5 15.5" stroke="white" stroke-width="2.2" stroke-linecap="round" fill="none" opacity="0.92"/>
      <!-- tusk -->
      <path d="M7.5 15.5C7 17.5 7.5 19.5 9.5 19.5" stroke="white" stroke-width="1.5" stroke-linecap="round" fill="none" opacity="0.75"/>
      <!-- eye ring -->
      <circle cx="16" cy="8.5" r="2.2" fill="#336791"/>
      <!-- pupil -->
      <circle cx="16.3" cy="8.5" r="1" fill="white" opacity="0.9"/>
      <!-- legs -->
      <rect x="10" y="17" width="2.5" height="4" rx="1.2" fill="white" opacity="0.75"/>
      <rect x="13.5" y="17" width="2.5" height="4" rx="1.2" fill="white" opacity="0.75"/>
    </g>

    <!-- ── MySQL: dolphin ─────────────────────────────────────── -->
    <g v-else-if="driver === 'mysql'">
      <!-- body -->
      <path d="M6 16C5 13 5 10 7 8C9 6 12 5.5 14 6C16 6.5 18 8 18.5 10.5C19 13 17.5 15.5 15 17C13 18 10 18 8.5 17.5L7 21H9.5L10.5 18.5C11.5 18.8 12.5 19 13 19C16 19 19.5 17 20.5 13.5C21.5 10 19.5 6.5 17 5C15 3.5 12.5 3 10.5 3.5C8 4 5.5 5.5 4.5 8C3.5 10 3.5 13 5 16H6Z" fill="white" opacity="0.9"/>
      <!-- dorsal fin -->
      <path d="M12 5.5L10.5 2L15 5" fill="white" opacity="0.8"/>
      <!-- tail fork -->
      <path d="M7 21L5 18L8 17.5" fill="white" opacity="0.7"/>
      <!-- eye -->
      <circle cx="15.5" cy="9" r="1.2" fill="#e48e00"/>
      <!-- smile -->
      <path d="M7 14C8.5 15.5 11 16 13 15.5" stroke="#e48e00" stroke-width="1" stroke-linecap="round" fill="none" opacity="0.8"/>
    </g>

    <!-- ── MariaDB: sea lion ───────────────────────────────────── -->
    <g v-else-if="driver === 'mariadb'">
      <!-- body -->
      <path d="M7 18C6 15 5.5 12 7 9.5C8.5 7 11 5.5 13.5 5.5C16 5.5 18.5 7.5 19 10C19.5 12.5 18 15 16 16.5L15 21H13L12 17H11L10 21H8L7 18Z" fill="white" opacity="0.9"/>
      <!-- snout -->
      <ellipse cx="8" cy="13.5" rx="2.5" ry="2" fill="white" opacity="0.7"/>
      <!-- whiskers -->
      <path d="M6 12L3 11" stroke="white" stroke-width="1.3" stroke-linecap="round" opacity="0.8"/>
      <path d="M6 13.5L3 13.5" stroke="white" stroke-width="1.3" stroke-linecap="round" opacity="0.8"/>
      <path d="M6 15L3 16" stroke="white" stroke-width="1.3" stroke-linecap="round" opacity="0.8"/>
      <!-- eye -->
      <circle cx="15" cy="8.5" r="1.4" fill="#c0765a"/>
      <circle cx="15.4" cy="8.2" r="0.5" fill="white" opacity="0.9"/>
      <!-- nose -->
      <circle cx="6.5" cy="13.5" r="0.7" fill="#c0765a"/>
    </g>

    <!-- ── SQL Server: database cylinders ────────────────────── -->
    <g v-else-if="driver === 'mssql'">
      <ellipse cx="12" cy="5.5" rx="8" ry="2.5" fill="white" opacity="0.95"/>
      <path d="M4 5.5V10" stroke="white" stroke-width="0.5" opacity="0.6"/>
      <path d="M20 5.5V10" stroke="white" stroke-width="0.5" opacity="0.6"/>
      <ellipse cx="12" cy="10" rx="8" ry="2.5" fill="white" opacity="0.85"/>
      <path d="M4 10V14.5" stroke="white" stroke-width="0.5" opacity="0.6"/>
      <path d="M20 10V14.5" stroke="white" stroke-width="0.5" opacity="0.6"/>
      <ellipse cx="12" cy="14.5" rx="8" ry="2.5" fill="white" opacity="0.75"/>
      <path d="M4 14.5V18" stroke="white" stroke-width="0.5" opacity="0.5"/>
      <path d="M20 14.5V18" stroke="white" stroke-width="0.5" opacity="0.5"/>
      <ellipse cx="12" cy="18" rx="8" ry="2.5" fill="white" opacity="0.9"/>
    </g>

    <!-- ── SQLite: cylinder with feather/leaf ─────────────────── -->
    <g v-else-if="driver === 'sqlite'">
      <!-- database cylinder -->
      <ellipse cx="11" cy="5.5" rx="6" ry="2" fill="white" opacity="0.9"/>
      <rect x="5" y="5.5" width="12" height="13" fill="white" opacity="0.75"/>
      <ellipse cx="11" cy="18.5" rx="6" ry="2" fill="white" opacity="0.9"/>
      <!-- feather accent -->
      <path d="M15 2C18 1 21 3.5 19.5 7C18 10.5 14 12 11 12.5" stroke="white" stroke-width="1.8" stroke-linecap="round" fill="none" opacity="0.9"/>
      <path d="M16.5 3.5L13 9" stroke="white" stroke-width="1.2" stroke-linecap="round" opacity="0.6"/>
      <path d="M18 5.5L14 10" stroke="white" stroke-width="1.2" stroke-linecap="round" opacity="0.5"/>
    </g>

    <!-- ── Redis: isometric cube (the "die") ─────────────────── -->
    <g v-else-if="driver === 'redis'">
      <!-- top face -->
      <path d="M12 2L21 7V8.5L12 13.5L3 8.5V7L12 2Z" fill="white" opacity="0.95"/>
      <!-- left face -->
      <path d="M3 8.5V15.5L12 20.5V13.5L3 8.5Z" fill="white" opacity="0.65"/>
      <!-- right face -->
      <path d="M21 8.5V15.5L12 20.5V13.5L21 8.5Z" fill="white" opacity="0.8"/>
      <!-- top face dots (die pattern) -->
      <circle cx="10.5" cy="6" r="0.9" fill="#c6302b"/>
      <circle cx="13.5" cy="7.5" r="0.9" fill="#c6302b"/>
      <circle cx="12" cy="9.5" r="0.9" fill="#c6302b"/>
    </g>

    <!-- ── Memcache: M letterform with speed lines ────────────── -->
    <g v-else-if="driver === 'memcache'">
      <!-- M shape -->
      <path d="M4 19V6L8.5 14L12 7.5L15.5 14L20 6V19" stroke="white" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" fill="none" opacity="0.95"/>
      <!-- speed underlines -->
      <path d="M3 21.5H13" stroke="white" stroke-width="1.6" stroke-linecap="round" opacity="0.55"/>
      <path d="M3 23.5H10" stroke="white" stroke-width="1.6" stroke-linecap="round" opacity="0.35"/>
    </g>

    <!-- ── Kafka: hub-and-spoke (3 satellite nodes) ───────────── -->
    <g v-else-if="driver === 'kafka'">
      <!-- spokes -->
      <line x1="12" y1="9.5" x2="12" y2="5.5" stroke="white" stroke-width="1.8" stroke-linecap="round" opacity="0.85"/>
      <line x1="9.5" y1="13.5" x2="6" y2="17" stroke="white" stroke-width="1.8" stroke-linecap="round" opacity="0.85"/>
      <line x1="14.5" y1="13.5" x2="18" y2="17" stroke="white" stroke-width="1.8" stroke-linecap="round" opacity="0.85"/>
      <!-- center hub -->
      <circle cx="12" cy="12" r="3" fill="white" opacity="0.95"/>
      <!-- satellite nodes -->
      <circle cx="12" cy="3.5" r="2" fill="white" opacity="0.85"/>
      <circle cx="5" cy="18.5" r="2" fill="white" opacity="0.85"/>
      <circle cx="19" cy="18.5" r="2" fill="white" opacity="0.85"/>
    </g>

    <!-- ── MongoDB: leaf over document store ──────────────────── -->
    <g v-else-if="driver === 'mongodb'">
      <ellipse cx="12" cy="5.5" rx="7" ry="2.5" fill="white" opacity="0.9"/>
      <path d="M5 5.5v10.5c0 1.4 3.1 2.5 7 2.5s7-1.1 7-2.5V5.5" fill="white" opacity="0.55"/>
      <ellipse cx="12" cy="16" rx="7" ry="2.5" fill="white" opacity="0.75"/>
      <path d="M12 3C15 6.2 15.5 10.5 12.2 14.5C9 10.5 9.3 6.2 12 3Z" fill="#00a35c" opacity="0.95"/>
      <path d="M12.1 5.6V17" stroke="white" stroke-width="1.3" stroke-linecap="round" opacity="0.8"/>
    </g>

    <!-- ── Elasticsearch: search cluster rings ────────────────── -->
    <g v-else-if="driver === 'elasticsearch'">
      <circle cx="10" cy="10" r="5.5" stroke="white" stroke-width="2.2" fill="none" opacity="0.95"/>
      <path d="M14.2 14.2L20 20" stroke="white" stroke-width="2.5" stroke-linecap="round" opacity="0.9"/>
      <path d="M6.5 10H13.5" stroke="white" stroke-width="1.8" stroke-linecap="round" opacity="0.75"/>
      <path d="M10 6.5V13.5" stroke="white" stroke-width="1.8" stroke-linecap="round" opacity="0.55"/>
    </g>

    <!-- ── OpenSearch: open lens wave ─────────────────────────── -->
    <g v-else-if="driver === 'opensearch'">
      <circle cx="10" cy="10" r="5.5" stroke="white" stroke-width="2.2" fill="none" opacity="0.95"/>
      <path d="M14.2 14.2L20 20" stroke="white" stroke-width="2.5" stroke-linecap="round" opacity="0.9"/>
      <path d="M4.5 15.5C7.5 13.5 10.5 13.5 13.5 15.5C15.5 16.8 17.2 17 19.5 15.8" stroke="white" stroke-width="1.7" stroke-linecap="round" fill="none" opacity="0.75"/>
    </g>

    <!-- ── AWS S3: bucket ─────────────────────────────────────── -->
    <g v-else-if="driver === 's3_aws'">
      <!-- handle arc -->
      <path d="M8.5 5C8.5 3 15.5 3 15.5 5" stroke="white" stroke-width="1.5" fill="none" stroke-linecap="round" opacity="0.8"/>
      <!-- bucket rim -->
      <ellipse cx="12" cy="7" rx="6.5" ry="2" fill="white" opacity="0.9"/>
      <!-- bucket body -->
      <path d="M5.5 7L7 20H17L18.5 7" fill="white" opacity="0.85"/>
      <!-- vertical seam lines (S3 style) -->
      <line x1="9.5" y1="9" x2="8.8" y2="19" stroke="#f59e0b" stroke-width="1.2" opacity="0.55"/>
      <line x1="14.5" y1="9" x2="15.2" y2="19" stroke="#f59e0b" stroke-width="1.2" opacity="0.55"/>
    </g>

    <!-- ── GCP Cloud Storage: cloud with Google colors ────────── -->
    <g v-else-if="driver === 's3_gcp'">
      <!-- cloud shape -->
      <path d="M18 10.5C17.8 8.3 16 6.5 13.5 6.5C12.6 6.5 11.8 6.8 11.2 7.2C10.5 5.4 8.8 4 6.8 4C4.2 4 2 6.2 2 9C2 9.1 2 9.2 2 9.3C1.4 9.8 1 10.6 1 11.5C1 13.4 2.6 15 4.5 15H18.5C20.4 15 22 13.4 22 11.5C22 10.1 21.1 8.9 19.8 8.4C19.5 9.3 19 10.1 18 10.5Z" fill="white" opacity="0.9"/>
      <!-- Google G symbol below cloud -->
      <path d="M12 17V20H14.5C14 21 13.1 21.5 12 21.5C10.3 21.5 9 20.2 9 18.5C9 16.8 10.3 15.5 12 15.5C12.8 15.5 13.5 15.8 14 16.3L15.8 14.5C14.8 13.6 13.5 13 12 13C9.2 13 7 15.2 7 18C7 20.8 9.2 23 12 23C14.8 23 17 20.8 17 18V17H12Z" fill="white" opacity="0.75"/>
    </g>

    <!-- ── Alibaba OSS: layered cloud ─────────────────────────── -->
    <g v-else-if="driver === 's3_oss'">
      <!-- main cloud -->
      <path d="M17.5 9.5C17.3 7.5 15.6 6 13.5 6C12.7 6 12 6.2 11.4 6.7C10.6 5.1 9 4 7 4C4.2 4 2 6.2 2 9C2 9.1 2 9.2 2 9.3C1.4 9.8 1 10.6 1 11.5C1 13.4 2.6 15 4.5 15H17.5C19.4 15 21 13.4 21 11.5C21 9.8 19.8 8.4 18.2 8C18 8.5 17.8 9 17.5 9.5Z" fill="white" opacity="0.9"/>
      <!-- wave lines (Alibaba style) -->
      <path d="M5 18C6.5 17 8.5 17 10 18C11.5 19 13.5 19 15 18C16.5 17 18.5 17 20 18" stroke="white" stroke-width="1.6" stroke-linecap="round" fill="none" opacity="0.75"/>
      <path d="M5 21C6.5 20 8.5 20 10 21C11.5 22 13.5 22 15 21C16.5 20 18.5 20 20 21" stroke="white" stroke-width="1.6" stroke-linecap="round" fill="none" opacity="0.5"/>
    </g>

    <!-- ── Huawei OBS: 6-petal flower ─────────────────────────── -->
    <g v-else-if="driver === 's3_obs'">
      <!-- petals (6, evenly spaced) -->
      <ellipse cx="12" cy="5.5" rx="2.2" ry="3.8" fill="white" opacity="0.9"/>
      <ellipse cx="12" cy="5.5" rx="2.2" ry="3.8" fill="white" opacity="0.9" transform="rotate(60 12 12)"/>
      <ellipse cx="12" cy="5.5" rx="2.2" ry="3.8" fill="white" opacity="0.9" transform="rotate(120 12 12)"/>
      <ellipse cx="12" cy="5.5" rx="2.2" ry="3.8" fill="white" opacity="0.9" transform="rotate(180 12 12)"/>
      <ellipse cx="12" cy="5.5" rx="2.2" ry="3.8" fill="white" opacity="0.9" transform="rotate(240 12 12)"/>
      <ellipse cx="12" cy="5.5" rx="2.2" ry="3.8" fill="white" opacity="0.9" transform="rotate(300 12 12)"/>
      <!-- center disc -->
      <circle cx="12" cy="12" r="3" fill="white" opacity="0.95"/>
    </g>

  </svg>
</template>
