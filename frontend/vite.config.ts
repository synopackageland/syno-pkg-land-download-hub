import {defineConfig} from 'vite'
import vue from '@vitejs/plugin-vue'
export default defineConfig({base:'./',define:{'process.env.NODE_ENV':JSON.stringify('production')},plugins:[vue()],build:{lib:{entry:'src/main.ts',formats:['es'],fileName:()=> 'app.module.js'},cssCodeSplit:false,outDir:'../ui',emptyOutDir:false,rollupOptions:{output:{chunkFileNames:'chunks/[name]-[hash].js',assetFileNames:a=>a.name?.endsWith('.css')?'app.css':'[name][extname]'}}}})
