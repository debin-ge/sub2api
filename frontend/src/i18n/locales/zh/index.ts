import adminAccessKeys from './adminAccessKeys'
import mcp from './mcp'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import videoPlayground from './videoPlayground'
import admin from './admin'
import misc from './misc'
import local from './local'
import { mergeLocaleFallbacks } from '../merge'

const upstream = {
  adminAccessKeys,
  mcp,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  ...videoPlayground,
  admin,
  ...misc,
}

export default mergeLocaleFallbacks(upstream, local)
