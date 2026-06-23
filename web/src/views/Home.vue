<template>
  <div
    class="bg-odin h-16 border-b border-black/10 flex items-center justify-between px-6 text-white"
  >
    <div class="flex items-center">
      <div class="flex items-center justify-center gap-x-4">
        <h1 class="font-semibold text-lg">Odin Playground</h1>
      </div>
      <div class="flex items-center justify-center gap-x-2 ml-4">
        <span>font size</span>
        <input
          type="range"
          @input="change_font_size"
          :min="font_size - 10"
          max="50"
          :value="font_size"
        />
      </div>
      <div class="flex items-center justify-center gap-x-2 ml-4">
        <button
          @click.stop="toggle_docs"
          class="border px-2 rounded-md cursor-pointer"
          :class="[docs_visible ? 'bg-gray-100 text-odin' : '']"
        >
          {{ docs_visible ? 'hide' : 'show' }} docs
        </button>
      </div>
      <div class="flex items-center justify-center gap-x-2 ml-4">
        <button
          @click.stop="run_code"
          class="border px-2 rounded-md cursor-pointer flex items-center gap-x-2"
        >
          <Play v-if="!compiling" :size="14" />
          <Loader v-if="compiling" class="animate-spin" :size="14" />
          Run
        </button>
      </div>
    </div>

    <div class="flex items-center justify-end">
      <img src="/odin-icon.svg" alt="Odin Programming Language" class="h-8 w-auto" />
    </div>
  </div>

  <div class="grid grid-cols-8 h-[calc(100vh-64px)]">
    <ResizablePanelGroup direction="horizontal" class="max-w-max border md:min-w-screen">
      <ResizablePanel :default-size="70" class="-mx-6">
        <div id="editor" class="flex h-screen items-center justify-center p-6 mt-4"></div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel :default-size="30" class="border-0 rounded-none! w-full">
        <div class="flex h-screen rounded-none!">
          <div
            class="bg-odin h-full w-full p-4 overflow-auto font-mono whitespace-pre text-sm text-[#ffff00]"
            style="
              text-shadow:
                0 0 0.5px #ffff66,
                0 0 0.5px #ffff66;
            "
          >
            <div class="font-bold mb-4 text-white">Output:</div>
            <div v-if="!compiling">
              {{ prog_output ?? '' }}
            </div>
            <div v-else>waiting for remote server...</div>
          </div>
        </div>
      </ResizablePanel>
      <ResizableHandle />
      <ResizablePanel :default-size="20" v-show="docs_visible">
        <div class="flex h-screen items-center justify-center">
          <iframe src="https://pkg.odin-lang.org/" frameborder="0" width="100%" height="100%" />
        </div>
      </ResizablePanel>
    </ResizablePanelGroup>
  </div>
</template>
<script setup lang="ts">
import { markRaw, onMounted, ref, shallowRef, watch } from 'vue'
import * as monaco from 'monaco-editor'
import ResizablePanelGroup from '@/components/ui/resizable/ResizablePanelGroup.vue'
import ResizablePanel from '@/components/ui/resizable/ResizablePanel.vue'
import ResizableHandle from '@/components/ui/resizable/ResizableHandle.vue'
import { Loader, Play } from '@lucide/vue'
const font_size = ref<number>(18)
const editor = shallowRef<monaco.editor.IStandaloneCodeEditor | null>(null)
const docs_visible = ref<boolean>(false)
const prog_output = ref<string>('')
const compiling = ref<boolean>(false)

const code = ref<string>(`package main

import "core:log"

main :: proc() {
    logger := log.create_console_logger()
	context.logger = logger
	log.infof("hellope")
}`)

type code_resp = {
  output: string
}

async function run_code() {
  if (!editor.value) {
    return
  }

  let code = editor.value.getValue()
  compiling.value = true
  let resp = await fetch('http://localhost:8080/api/code', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      code,
    }),
  })

  let o_obj: code_resp = await resp.json()
  prog_output.value = o_obj.output
  compiling.value = false
}

function change_font_size(evt: Event) {
  const value = (evt.target as HTMLInputElement).value
  console.log(value)
  if (!editor.value) {
    return
  }
  editor.value.updateOptions({
    fontSize: +value,
  })
}

function toggle_docs() {
  docs_visible.value = !docs_visible.value
}

onMounted(() => {
  const ed = monaco.editor.create(document.querySelector('#editor')!, {
    value: code.value,
    language: 'odin',
    automaticLayout: true,
    theme: 'vs',
    minimap: { enabled: false },
    scrollbar: {
      vertical: 'auto',
      horizontal: 'auto',
      verticalScrollbarSize: 15,
      horizontalScrollbarSize: 15,
    },
    guides: {
      indentation: false,
      bracketPairs: 'active',
      bracketPairsHorizontal: false,
    },
    fontFamily: 'JetBrains Mono',
    fontSize: font_size.value,
    overviewRulerLanes: 0,
    hideCursorInOverviewRuler: true,
    lineNumbersMinChars: 3,
    lineDecorationsWidth: 0,
  })

  editor.value = markRaw(ed)
})
</script>
