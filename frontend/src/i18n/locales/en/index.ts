import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import admin from './admin'
import misc from './misc'
import local from './local'
import { mergeLocaleFallbacks } from '../merge'

const upstream = {
  ...landing,
  ...common,
  ...dashboard,
  admin,
  ...misc,
}

export default mergeLocaleFallbacks(upstream, local)
