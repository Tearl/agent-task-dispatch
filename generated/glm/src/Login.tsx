import React from 'react';

export default function Login() {
  return (
    <div className="min-h-screen bg-slate-900 relative overflow-hidden flex items-center justify-center">
      {/* Grid background */}
      <div className="absolute inset-0 opacity-20">
        <div className="absolute inset-0" style={{
          backgroundImage: `linear-gradient(rgba(59, 130, 246, 0.1) 1px, transparent 1px), linear-gradient(90deg, rgba(59, 130, 246, 0.1) 1px, transparent 1px)`,
          backgroundSize: '20px 20px'
        }}></div>
      </div>
      
      {/* Side light effects */}
      <div className="absolute left-0 top-1/2 transform -translate-y-1/2 w-32 h-32 bg-blue-500 rounded-full blur-3xl opacity-30"></div>
      <div className="absolute right-0 top-1/2 transform -translate-y-1/2 w-32 h-32 bg-blue-500 rounded-full blur-3xl opacity-30"></div>
      
      {/* Main login container */}
      <div className="relative z-10 bg-slate-800 bg-opacity-80 p-8 rounded-2xl border-2 border-pink-500 border-opacity-50 shadow-2xl"
           style={{
             boxShadow: '0 0 20px rgba(236, 72, 153, 0.5), inset 0 0 20px rgba(59, 130, 246, 0.3)',
             borderImage: 'linear-gradient(45deg, #ec4899, #3b82f6) 1'
           }}>
        
        {/* Title */}
        <h2 className="text-white text-3xl font-bold text-center mb-8">用户名</h2>
        
        {/* Username input */}
        <div className="mb-6 relative">
          <input 
            type="text" 
            placeholder="请输入用户名" 
            className="w-full px-4 py-3 bg-slate-700 bg-opacity-50 border-2 border-cyan-400 rounded-lg text-white placeholder-cyan-300 focus:outline-none focus:border-cyan-300 transition-colors"
            style={{
              boxShadow: '0 0 10px rgba(6, 182, 212, 0.5)'
            }}
          />
          {/* Cursor icon */}
          <div className="absolute right-3 top-1/2 transform -translate-y-1/2 text-white text-2xl">
            ⇅
          </div>
        </div>
        
        {/* Password label */}
        <div className="text-white text-xl mb-2">密码</div>
        
        {/* Password input */}
        <div className="mb-8 relative">
          <input 
            type="password" 
            placeholder="请输入密码" 
            className="w-full px-4 py-3 bg-slate-700 bg-opacity-50 border-2 border-cyan-400 rounded-lg text-white placeholder-cyan-300 focus:outline-none focus:border-cyan-300 transition-colors"
            style={{
              boxShadow: '0 0 10px rgba(6, 182, 212, 0.5)'
            }}
          />
        </div>
        
        {/* Login button */}
        <button className="w-full py-4 bg-gradient-to-r from-purple-600 to-pink-600 text-white text-2xl font-bold rounded-lg hover:from-purple-700 hover:to-pink-700 transition-all duration-300"
                style={{
                  boxShadow: '0 0 15px rgba(236, 72, 153, 0.7)'
                }}>
          登录
        </button>
      </div>
      
      {/* AI generated watermark */}
      <div className="absolute bottom-4 right-4 text-gray-500 text-sm">
        AI生成
      </div>
    </div>
  );
}