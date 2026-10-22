<script setup lang="ts">
// 初始化管理员（方案 §3「/setup | 初始化管理员 | 向导表单」）。
//
// 这个视图只是向导的宿主：真正的表单与步骤逻辑在 SetupWizard 组件里。
// 为什么要分文件：方案把 /setup 与 /login 列成两行、给 /setup 点名了「向导表单」，
// 让 Login.vue 靠 phase 兼职两角，等于 /setup 那一行是空的。
import SetupWizard from '@/components/auth/SetupWizard.vue'
</script>

<template>
  <SetupWizard />
</template>
