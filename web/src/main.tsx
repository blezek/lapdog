import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import { QueryClientProvider } from '@tanstack/react-query'

import { App } from './App'
import { createQueryClient } from './queryClient'
import './styles.css'

const queryClient = createQueryClient()

const host = document.getElementById('root')
if (!host) throw new Error('no #root element in the document')

createRoot(host).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
